//go:build linux

package personalguest

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	guest "github.com/opensoha/soha-contracts/agent/guest"
)

func boundedUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

type browserPipe struct {
	command                 *exec.Cmd
	input, output           *os.File
	reader                  *bufio.Reader
	id                      int64
	session, control, owner string
	generation              int64
	broker                  *runtimeBroker
	mu                      sync.Mutex
	writes                  sync.Mutex
	pending                 map[int64]chan browserReply
	done                    chan struct{}
	network                 chan struct{}
	operation               *guest.RuntimeOperation
	operationCtx            context.Context
	localURL                string
}

type browserReply struct {
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

func (b *runtimeBroker) browser(ctx context.Context, op guest.RuntimeOperation) (*guest.BrowserFrame, error) {
	action := op.Browser
	if action == nil || action.Validate() != nil {
		return nil, errors.New("invalid browser operation")
	}
	if b.page == nil {
		if action.Action != "acquire" {
			return nil, errors.New("browser requires fresh control after restart")
		}
		page, err := b.startBrowser(b.ctx)
		if err != nil {
			return nil, err
		}
		b.page = page
	}
	page := b.page
	page.mu.Lock()
	page.operation = &op
	page.operationCtx = ctx
	page.mu.Unlock()
	defer func() { page.mu.Lock(); page.operation = nil; page.operationCtx = nil; page.mu.Unlock() }()
	if action.Action == "acquire" {
		if page.control != "" && (page.control != action.ControlID || action.Generation != page.generation+1) {
			return nil, errors.New("browser control already owned")
		}
		page.control, page.generation, page.owner = action.ControlID, action.Generation, action.Owner
	} else if action.Action == "takeover" || action.Action == "return" {
		if page.control != action.ControlID || action.Generation != page.generation+1 {
			return nil, errors.New("stale browser handover")
		}
		page.generation, page.owner = action.Generation, action.Owner
	} else if page.control != action.ControlID || page.generation != action.Generation || page.owner != action.Owner {
		return nil, errors.New("stale browser control")
	}
	if err := page.act(ctx, action); err != nil {
		page.close()
		b.page = nil
		return nil, err
	}
	frame, err := page.frame(ctx, b.instance)
	if err != nil {
		page.close()
		b.page = nil
		return nil, err
	}
	if action.Action == "release" {
		page.control = ""
		page.owner = ""
	}
	return frame, err
}

func (b *runtimeBroker) startBrowser(ctx context.Context) (*browserPipe, error) {
	inputReader, inputWriter, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outputReader, outputWriter, err := os.Pipe()
	if err != nil {
		_ = inputReader.Close()
		_ = inputWriter.Close()
		return nil, err
	}
	args := sandboxArgs(runtimeWorkspace(b.plan), "", false)
	args = args[:len(args)-3]
	args = append(args, "--bind", filepath.Join(filepath.Dir(runtimeWorkspace(b.plan)), "browser"), "/profile", "--chdir", "/data", "--", "/usr/bin/chromium", "--headless=new", "--disable-setuid-sandbox", "--remote-debugging-pipe", "--user-data-dir=/profile", "--no-first-run", "--no-default-browser-check", "--disable-background-networking", "--disable-component-update", "--disable-sync", "--disable-gpu", "--disable-dev-shm-usage", "--renderer-process-limit=1", "--window-size=800,600", "about:blank")
	command := exec.CommandContext(ctx, "/usr/bin/bwrap", args...) //nolint:gosec // Fixed browser and isolated filesystem; caller flags and CDP stay private.
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "HOME=/home/soha"}
	command.ExtraFiles = []*os.File{inputReader, outputWriter}
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Start(); err != nil {
		for _, file := range []*os.File{inputReader, inputWriter, outputReader, outputWriter} {
			_ = file.Close()
		}
		return nil, errors.New("isolated browser unavailable")
	}
	_ = inputReader.Close()
	_ = outputWriter.Close()
	page := &browserPipe{command: command, input: inputWriter, output: outputReader, reader: bufio.NewReaderSize(outputReader, 64<<10), broker: b, pending: map[int64]chan browserReply{}, done: make(chan struct{}), network: make(chan struct{}, 8)}
	go func() { _ = command.Wait(); _ = inputWriter.Close(); _ = outputReader.Close() }()
	go page.receive()
	prepareCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var target struct {
		TargetID string `json:"targetId"`
	}
	if err := page.call(prepareCtx, "Target.createTarget", map[string]any{"url": "about:blank"}, false, &target); err != nil {
		page.close()
		return nil, err
	}
	var session struct {
		SessionID string `json:"sessionId"`
	}
	if err := page.call(prepareCtx, "Target.attachToTarget", map[string]any{"targetId": target.TargetID, "flatten": true}, false, &session); err != nil {
		page.close()
		return nil, err
	}
	page.mu.Lock()
	page.session = session.SessionID
	page.mu.Unlock()
	if err := page.call(prepareCtx, "Page.enable", map[string]any{}, true, nil); err != nil {
		page.close()
		return nil, err
	}
	if err := page.call(prepareCtx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 800, "height": 600, "deviceScaleFactor": 1, "mobile": false}, true, nil); err != nil {
		page.close()
		return nil, err
	}
	if err := page.call(prepareCtx, "Fetch.enable", map[string]any{"patterns": []map[string]string{{"urlPattern": "*", "requestStage": "Request"}}}, true, nil); err != nil {
		page.close()
		return nil, err
	}
	return page, nil
}

func (p *browserPipe) close() {
	if p.command.Process != nil {
		_ = p.command.Process.Kill()
	}
	_ = p.input.Close()
	_ = p.output.Close()
}

func (p *browserPipe) call(ctx context.Context, method string, params any, inPage bool, value any) error {
	deadline := time.Now().Add(10 * time.Second)
	if until, ok := ctx.Deadline(); ok && until.Before(deadline) {
		deadline = until
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	p.writes.Lock()
	if p.input.SetWriteDeadline(deadline) != nil {
		p.writes.Unlock()
		return errors.New("browser pipe cannot enforce deadlines")
	}
	p.mu.Lock()
	p.id++
	id := p.id
	reply := make(chan browserReply, 1)
	p.pending[id] = reply
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.pending, id); p.mu.Unlock() }()
	message := map[string]any{"id": id, "method": method, "params": params}
	if inPage {
		p.mu.Lock()
		message["sessionId"] = p.session
		p.mu.Unlock()
	}
	raw, err := json.Marshal(message)
	if err != nil {
		p.writes.Unlock()
		return err
	}
	if _, err = p.input.Write(append(raw, 0)); err != nil {
		p.writes.Unlock()
		return errors.New("browser control pipe unavailable")
	}
	p.writes.Unlock()
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case response := <-reply:
		if len(response.Error) > 0 {
			return errors.New("browser action rejected")
		}
		if value != nil && json.Unmarshal(response.Result, value) != nil {
			return errors.New("invalid browser receipt")
		}
		return nil
	case <-p.done:
		return errors.New("browser control pipe unavailable")
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errors.New("browser action outcome unconfirmed")
	}
}

func (p *browserPipe) receive() {
	defer close(p.done)
	for {
		raw, err := p.readMessage()
		if err != nil {
			return
		}
		var frame struct {
			ID        int64           `json:"id"`
			Method    string          `json:"method"`
			Params    json.RawMessage `json:"params"`
			SessionID string          `json:"sessionId"`
			browserReply
		}
		if json.Unmarshal(raw, &frame) != nil {
			return
		}
		if frame.ID != 0 {
			p.mu.Lock()
			reply := p.pending[frame.ID]
			p.mu.Unlock()
			if reply != nil {
				select {
				case reply <- frame.browserReply:
				default:
				}
			}
			continue
		}
		p.mu.Lock()
		session := p.session
		op, ctx := p.operation, p.operationCtx
		p.mu.Unlock()
		if frame.Method == "Fetch.requestPaused" && frame.SessionID == session {
			select {
			case p.network <- struct{}{}:
				go func(raw json.RawMessage) { defer func() { <-p.network }(); p.requestPaused(ctx, op, raw) }(frame.Params)
			default:
				go p.failPaused(frame.Params)
			}
		}
	}
}

func (p *browserPipe) failPaused(raw json.RawMessage) {
	var request struct {
		RequestID string `json:"requestId"`
	}
	if json.Unmarshal(raw, &request) != nil {
		return
	}
	ctx, cancel := context.WithTimeout(p.broker.ctx, 2*time.Second)
	defer cancel()
	_ = p.call(ctx, "Fetch.failRequest", map[string]any{"requestId": request.RequestID, "errorReason": "BlockedByClient"}, true, nil)
}

func (p *browserPipe) requestPaused(ctx context.Context, op *guest.RuntimeOperation, raw json.RawMessage) {
	var event struct {
		RequestID string `json:"requestId"`
		Request   struct {
			URL, Method, PostData string
			Headers               map[string]string
		} `json:"request"`
	}
	if json.Unmarshal(raw, &event) != nil || op == nil || ctx == nil || ctx.Err() != nil {
		p.failPaused(raw)
		return
	}
	if strings.HasPrefix(event.Request.URL, "file:") {
		p.failPaused(raw)
		return
	}
	p.mu.Lock()
	local := p.localURL != ""
	p.mu.Unlock()
	if local {
		p.failPaused(raw)
		return
	}
	input := guest.BrowserNetworkRequest{URL: event.Request.URL, Method: event.Request.Method, Headers: event.Request.Headers, BodyBase64: base64.StdEncoding.EncodeToString([]byte(event.Request.PostData))}
	result, err := p.broker.browserNetwork(ctx, *op, input)
	if err != nil {
		p.failPaused(raw)
		return
	}
	_ = p.call(ctx, "Fetch.fulfillRequest", map[string]any{"requestId": event.RequestID, "responseCode": result.Status, "body": result.BodyBase64, "responseHeaders": result.Headers}, true, nil)
}

func (p *browserPipe) readMessage() ([]byte, error) {
	var raw []byte
	for {
		part, err := p.reader.ReadSlice(0)
		if len(raw)+len(part) > 1<<20 {
			return nil, errors.New("browser control frame exceeded its bound")
		}
		raw = append(raw, part...)
		if err == nil {
			return raw[:len(raw)-1], nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return nil, errors.New("browser action outcome unconfirmed")
		}
	}
}

func (p *browserPipe) evaluate(ctx context.Context, expression string, value any) error {
	var result struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		Exception json.RawMessage `json:"exceptionDetails"`
	}
	if err := p.call(ctx, "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true, "awaitPromise": true}, true, &result); err != nil {
		return err
	}
	if len(result.Exception) > 0 || json.Unmarshal(result.Result.Value, value) != nil {
		return errors.New("bounded browser action failed")
	}
	return nil
}

func (p *browserPipe) act(ctx context.Context, action *guest.BrowserAction) error {
	switch action.Action {
	case "navigate":
		p.mu.Lock()
		p.localURL = ""
		p.mu.Unlock()
		if strings.HasPrefix(action.URL, "file:///data/") {
			return p.localPage(ctx, action.URL)
		}
		if err := p.call(ctx, "Page.navigate", map[string]any{"url": action.URL}, true, nil); err != nil {
			return err
		}
		return p.waitNavigation(ctx, action.URL)
	case "click", "type":
		selector, _ := json.Marshal(action.Selector)
		text, _ := json.Marshal(action.Text)
		body := fmt.Sprintf(`(()=>{const e=document.querySelector(%s);if(!e)return false;`, selector)
		if action.Owner == "agent" {
			body += `if(e.matches('input[type=password]'))return false;`
		}
		if action.Action == "type" {
			body += fmt.Sprintf(`if(!e.matches('input,textarea')||e.disabled)return false;e.value=%s;e.dispatchEvent(new Event('input',{bubbles:true}));`, text)
		} else {
			body += `e.click();`
		}
		body += `return true})()`
		var changed bool
		if err := p.evaluate(ctx, body, &changed); err != nil {
			return err
		}
		if !changed {
			return errors.New("browser target unavailable")
		}
		return nil
	case "pointer":
		params := map[string]any{"type": "mousePressed", "x": *action.X, "y": *action.Y, "button": "left", "clickCount": 1}
		if err := p.call(ctx, "Input.dispatchMouseEvent", params, true, nil); err != nil {
			return err
		}
		params["type"] = "mouseReleased"
		return p.call(ctx, "Input.dispatchMouseEvent", params, true, nil)
	case "snapshot", "acquire", "takeover", "return", "release":
		return nil
	}
	return errors.New("unsupported browser operation")
}

func (p *browserPipe) localPage(ctx context.Context, target string) error {
	u, err := url.Parse(target)
	if err != nil || !guest.BrowserURL(target) {
		return errors.New("invalid local browser target")
	}
	root, err := os.OpenRoot(runtimeWorkspace(p.broker.plan))
	if err != nil {
		return err
	}
	raw, err := readRuntimeFile(root, strings.TrimPrefix(u.Path, "/data/"))
	_ = root.Close()
	if err != nil {
		return err
	}
	if err = p.call(ctx, "Page.navigate", map[string]any{"url": "about:blank"}, true, nil); err != nil {
		return err
	}
	if err = p.waitNavigation(ctx, "about:blank"); err != nil {
		return err
	}
	var tree struct {
		FrameTree struct {
			Frame struct {
				ID string `json:"id"`
			} `json:"frame"`
		} `json:"frameTree"`
	}
	if err = p.call(ctx, "Page.getFrameTree", map[string]any{}, true, &tree); err != nil {
		return err
	}
	// An opaque about:blank document has no local-file origin. Untrusted HTML
	// cannot read the browser profile or credentials through file subresources.
	const policy = `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; form-action 'none'; frame-src 'none'; connect-src 'none'; base-uri 'none'">`
	p.mu.Lock()
	p.localURL = target
	p.mu.Unlock()
	if err = p.call(ctx, "Page.setDocumentContent", map[string]any{"frameId": tree.FrameTree.Frame.ID, "html": policy + string(raw)}, true, nil); err != nil {
		return err
	}
	return nil
}

func (p *browserPipe) waitNavigation(ctx context.Context, target string) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		var ready bool
		encoded, _ := json.Marshal(target)
		if err := p.evaluate(ctx, fmt.Sprintf(`(location.href===%s || location.protocol==='https:') && document.readyState!=='loading'`, encoded), &ready); err != nil {
			return err
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (p *browserPipe) frame(ctx context.Context, instance string) (*guest.BrowserFrame, error) {
	frame := &guest.BrowserFrame{ControlID: p.control, Generation: p.generation, InstanceID: instance, Width: 800, Height: 600}
	var content struct{ URL, Title, Text string }
	if err := p.evaluate(ctx, `(()=>({URL:location.href,Title:document.title.slice(0,512),Text:(document.body?.innerText||'').slice(0,4096)}))()`, &content); err != nil {
		return nil, err
	}
	if content.URL != "about:blank" && !guest.BrowserURL(content.URL) {
		return nil, errors.New("browser navigated beyond allowed targets")
	}
	frame.URL, frame.Title, frame.Text = content.URL, boundedUTF8(content.Title, 512), boundedUTF8(content.Text, 4096)
	p.mu.Lock()
	local := p.localURL
	p.mu.Unlock()
	if content.URL == "about:blank" && local != "" {
		frame.URL = local
	}
	var snapshot struct {
		Data string `json:"data"`
	}
	for _, quality := range []int{55, 25, 8} {
		if err := p.call(ctx, "Page.captureScreenshot", map[string]any{"format": "jpeg", "quality": quality, "captureBeyondViewport": false}, true, &snapshot); err != nil {
			return nil, err
		}
		if len(snapshot.Data) <= 48<<10 {
			frame.JPEGBase64 = snapshot.Data
			return frame, nil
		}
	}
	return nil, errors.New("browser frame exceeded its bound")
}
