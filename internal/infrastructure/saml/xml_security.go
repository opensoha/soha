package saml

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	maxXMLBytes    = 1 << 20
	maxXMLDepth    = 64
	maxXMLElements = 10000
)

var (
	ErrInvalidXML     = errors.New("invalid SAML XML")
	ErrUnsupportedAlg = errors.New("unsupported SAML signature algorithm")
)

type xmlShape struct {
	root        xml.Name
	rootID      string
	inResponse  string
	assertionID string
	assertions  int
	signatures  int
}

func inspectXML(data []byte, response bool) (xmlShape, error) {
	if len(data) == 0 || len(data) > maxXMLBytes {
		return xmlShape{}, fmt.Errorf("%w: document size must be between 1 and %d bytes", ErrInvalidXML, maxXMLBytes)
	}
	inspector := xmlInspector{response: response, ids: make(map[string]struct{}), parents: make([]xml.Name, 0, 16), parentIDs: make([]string, 0, 16)}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return xmlShape{}, fmt.Errorf("%w: %v", ErrInvalidXML, err)
		}
		switch value := token.(type) {
		case xml.Directive:
			return xmlShape{}, fmt.Errorf("%w: directives are not allowed", ErrInvalidXML)
		case xml.StartElement:
			if err := inspector.start(value); err != nil {
				return xmlShape{}, err
			}
		case xml.EndElement:
			if err := inspector.end(); err != nil {
				return xmlShape{}, err
			}
		}
	}
	if inspector.depth != 0 || inspector.shape.root.Local == "" {
		return xmlShape{}, fmt.Errorf("%w: incomplete XML", ErrInvalidXML)
	}
	return inspector.shape, nil
}

type xmlInspector struct {
	shape     xmlShape
	response  bool
	depth     int
	elements  int
	ids       map[string]struct{}
	parents   []xml.Name
	parentIDs []string
}

func (i *xmlInspector) start(element xml.StartElement) error {
	i.depth++
	i.elements++
	if i.depth > maxXMLDepth || i.elements > maxXMLElements {
		return fmt.Errorf("%w: XML complexity limit exceeded", ErrInvalidXML)
	}
	id := attribute(element.Attr, "ID")
	if err := i.recordID(id); err != nil {
		return err
	}
	i.recordShape(element, id)
	if err := i.recordSignature(element); err != nil {
		return err
	}
	if err := validateAlgorithm(element, i.parents); err != nil {
		return err
	}
	if err := i.validateReference(element); err != nil {
		return err
	}
	i.parents = append(i.parents, element.Name)
	i.parentIDs = append(i.parentIDs, id)
	return nil
}

func (i *xmlInspector) recordID(id string) error {
	if id == "" {
		return nil
	}
	if _, exists := i.ids[id]; exists {
		return fmt.Errorf("%w: duplicate ID", ErrInvalidXML)
	}
	i.ids[id] = struct{}{}
	return nil
}

func (i *xmlInspector) recordShape(element xml.StartElement, id string) {
	if i.depth == 1 {
		i.shape.root = element.Name
		i.shape.rootID = id
		i.shape.inResponse = attribute(element.Attr, "InResponseTo")
	}
	if i.response && i.depth == 2 && element.Name.Space == assertionNamespace && (element.Name.Local == "Assertion" || element.Name.Local == "EncryptedAssertion") {
		i.shape.assertions++
		if element.Name.Local == "Assertion" {
			i.shape.assertionID = id
		}
	}
}

func (i *xmlInspector) recordSignature(element xml.StartElement) error {
	if element.Name.Space != signatureNamespace || element.Name.Local != "Signature" {
		return nil
	}
	if i.depth < 2 || !allowedSignatureParent(i.parents[len(i.parents)-1], i.depth, i.response) {
		return fmt.Errorf("%w: signature is not attached to Response or Assertion", ErrInvalidXML)
	}
	i.shape.signatures++
	return nil
}

func (i *xmlInspector) validateReference(element xml.StartElement) error {
	if element.Name.Space != signatureNamespace || element.Name.Local != "Reference" {
		return nil
	}
	uri := attribute(element.Attr, "URI")
	if len(i.parentIDs) < 3 || i.parentIDs[len(i.parentIDs)-3] == "" || uri != "#"+i.parentIDs[len(i.parentIDs)-3] {
		return fmt.Errorf("%w: signature reference does not target its parent", ErrInvalidXML)
	}
	return nil
}

func (i *xmlInspector) end() error {
	if i.depth == 0 || len(i.parents) == 0 {
		return fmt.Errorf("%w: unbalanced XML", ErrInvalidXML)
	}
	i.parents = i.parents[:len(i.parents)-1]
	i.parentIDs = i.parentIDs[:len(i.parentIDs)-1]
	i.depth--
	return nil
}

func attribute(attributes []xml.Attr, name string) string {
	for _, item := range attributes {
		if item.Name.Space == "" && item.Name.Local == name {
			return item.Value
		}
	}
	return ""
}

func allowedSignatureParent(parent xml.Name, depth int, response bool) bool {
	return depth == 2 && parent.Space == protocolNamespace && (parent.Local == "Response" || !response && parent.Local == "AuthnRequest") ||
		depth == 3 && parent.Space == assertionNamespace && parent.Local == "Assertion"
}

func validateAlgorithm(element xml.StartElement, parents []xml.Name) error {
	if element.Name.Space != signatureNamespace {
		return nil
	}
	algorithm := attribute(element.Attr, "Algorithm")
	if algorithm == "" {
		return nil
	}
	if strings.Contains(strings.ToLower(algorithm), "sha1") {
		return fmt.Errorf("%w: SHA-1 is not allowed", ErrUnsupportedAlg)
	}
	var allowed map[string]struct{}
	switch element.Name.Local {
	case "SignatureMethod":
		allowed = allowedSignatureAlgorithms
	case "DigestMethod":
		allowed = allowedDigestAlgorithms
	case "CanonicalizationMethod", "Transform":
		allowed = allowedTransformAlgorithms
	default:
		return nil
	}
	if _, ok := allowed[algorithm]; !ok {
		return fmt.Errorf("%w: %s", ErrUnsupportedAlg, algorithm)
	}
	if element.Name.Local == "SignatureMethod" && !hasParent(parents, "SignedInfo") {
		return fmt.Errorf("%w: misplaced signature algorithm", ErrInvalidXML)
	}
	return nil
}

func hasParent(parents []xml.Name, local string) bool {
	return len(parents) > 0 && parents[len(parents)-1].Space == signatureNamespace && parents[len(parents)-1].Local == local
}

var allowedSignatureAlgorithms = map[string]struct{}{
	"http://www.w3.org/2001/04/xmldsig-more#rsa-sha256":   {},
	"http://www.w3.org/2001/04/xmldsig-more#rsa-sha384":   {},
	"http://www.w3.org/2001/04/xmldsig-more#rsa-sha512":   {},
	"http://www.w3.org/2001/04/xmldsig-more#ecdsa-sha256": {},
	"http://www.w3.org/2001/04/xmldsig-more#ecdsa-sha384": {},
	"http://www.w3.org/2001/04/xmldsig-more#ecdsa-sha512": {},
}

var allowedDigestAlgorithms = map[string]struct{}{
	"http://www.w3.org/2001/04/xmlenc#sha256":       {},
	"http://www.w3.org/2001/04/xmldsig-more#sha384": {},
	"http://www.w3.org/2001/04/xmlenc#sha512":       {},
}

var allowedTransformAlgorithms = map[string]struct{}{
	"http://www.w3.org/2000/09/xmldsig#enveloped-signature": {},
	"http://www.w3.org/2001/10/xml-exc-c14n#":               {},
	"http://www.w3.org/2001/10/xml-exc-c14n#WithComments":   {},
}

const (
	protocolNamespace  = "urn:oasis:names:tc:SAML:2.0:protocol"
	assertionNamespace = "urn:oasis:names:tc:SAML:2.0:assertion"
	signatureNamespace = "http://www.w3.org/2000/09/xmldsig#"
)
