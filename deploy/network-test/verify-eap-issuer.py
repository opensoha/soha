#!/usr/bin/env python3
"""Dedicated EAP-only certificate lifecycle in the owned software RADIUS lab."""

from datetime import datetime, timedelta, timezone
import importlib.util
import json
from pathlib import Path
import re
import ssl
import subprocess
import uuid
import urllib.error

spec = importlib.util.spec_from_file_location('radius_lab', Path(__file__).with_name('radius-lab.py'))
lab = importlib.util.module_from_spec(spec)
spec.loader.exec_module(lab)


def main():
    lab.owned_state()
    seed = json.loads((lab.STATE / 'seed.json').read_text())
    token = lab.admin_token()
    capabilities = ['radius-eap-tls-v1', 'endpoint-presence-v1', 'eap-profile-v1']
    controller = lab.STATE / 'tls' / 'eap-client'
    marker = lab.STATE / 'eap-profile-enrollment.json'
    if not marker.exists():
        backup = lab.STATE / 'tls' / 'eap-client-before-profile'
        if not backup.exists():
            controller.rename(backup)
        lab.certificate('eap-client', seed['subjectId'],
                        uri='spiffe://opensoha.local/network-control/endpoint/' + lab.DEVICE_ID)
        lab.write(controller / 'eapol-test.conf', (backup / 'eapol-test.conf').read_text())
        lab.enroll(token, lab.DEVICE_ID, 'endpoint', seed['subjectId'], lab.DEVICE_ID, 'eap-client', capabilities)
        lab.write(marker, {'runtimeId': lab.DEVICE_ID})
    context = ssl.create_default_context(cafile=str(controller / 'ca.crt'))
    context.load_cert_chain(str(controller / 'tls.crt'), str(controller / 'tls.key'))
    runtime_path = '/runtimes/' + lab.DEVICE_ID
    origin = 'https://127.0.0.1:18182/api/network-control/v1'

    def call(action, message_type, payload):
        now = datetime.now(timezone.utc)
        message = {'schemaVersion': 'network-runtime/v1alpha1', 'messageId': str(uuid.uuid4()),
                   'messageType': message_type, 'producerId': lab.DEVICE_ID,
                   'runtimeId': lab.DEVICE_ID, 'runtimeKind': 'endpoint', 'occurredAt': now.isoformat(),
                   'expiresAt': (now + timedelta(minutes=1)).isoformat(), 'payload': payload}
        return lab.request('POST', runtime_path + '/' + action, body=message, context=context, origin=origin)['payload']

    issued = lab.STATE / 'tls' / 'eap-issued-client'
    issued.mkdir(exist_ok=True, mode=0o700)
    # Reuse one request on retries; never copy the controller private key.
    if not (issued / 'request.json').exists():
        lab.run('openssl', 'genpkey', '-algorithm', 'EC', '-pkeyopt', 'ec_paramgen_curve:P-256', '-out', str(issued / 'tls.key'))
        (issued / 'tls.key').chmod(0o600)
        lab.run('openssl', 'req', '-new', '-key', str(issued / 'tls.key'), '-subj', '/CN=ignored-csr-subject', '-out', str(issued / 'request.pem'))
        lab.write(issued / 'request.json', {'requestId': str(uuid.uuid4()), 'csrPem': (issued / 'request.pem').read_text()})
    request = json.loads((issued / 'request.json').read_text())
    result = call('eap:certificate', 'eap.certificate.request', request)
    previous_certificate = None
    if result['status'] == 'active':
        previous_certificate = result['certificatePem']
        request['requestId'] = str(uuid.uuid4())
        lab.write(issued / 'request.json', request)
        result = call('eap:certificate', 'eap.certificate.request', request)
    replay = call('eap:certificate', 'eap.certificate.request', request)
    assert result == replay and result['status'] == 'pending'
    lab.write(issued / 'tls.crt', result['certificatePem'])
    lab.write(issued / 'issuer.crt', result['caCertificatePem'])
    lab.write(issued / 'ca.crt', (controller / 'ca.crt').read_text())
    lab.write(issued / 'eapol-test.conf', (controller / 'eapol-test.conf').read_text())
    certificate = lab.run('openssl', 'x509', '-in', str(issued / 'tls.crt'), '-noout', '-text')
    assert 'spiffe://opensoha.local/network-eap/endpoint/' + lab.DEVICE_ID in certificate
    assert '/network-control/' not in certificate and 'ignored-csr-subject' not in certificate
    lab.run('openssl', 'verify', '-CAfile', str(issued / 'issuer.crt'), '-purpose', 'sslclient', str(issued / 'tls.crt'))
    assert lab.run('openssl', 'pkey', '-in', str(issued / 'tls.key'), '-pubout') != lab.run('openssl', 'pkey', '-in', str(controller / 'tls.key'), '-pubout')

    def authenticate(allowed, configuration='eapol-test.conf'):
        try:
            output = lab.compose('run', '--rm', '--no-deps', '-e',
                                 'SOHA_RADIUS_EAP_TEST_CONFIG_FILE=/run/soha-radius/eap-test/' + configuration,
                                 'eap-issued-client')
        except subprocess.CalledProcessError as error:
            assert not allowed and 'Access-Reject' in error.stdout
        else:
            assert allowed and re.search(r'^SUCCESS$', output, re.MULTILINE)

    authenticate(False)
    activation = {'credentialId': result['credentialId'], 'fingerprint': result['fingerprint']}
    try:
        call('eap:activate', 'eap.certificate.activate.request', {**activation, 'fingerprint': 'sha256:' + '0' * 64})
    except RuntimeError as error:
        assert 'HTTP 409' in str(error)
    else:
        raise AssertionError('A different fingerprint activated the EAP certificate')
    active = call('eap:activate', 'eap.certificate.activate.request', activation)
    assert active['status'] == 'active' and active['credentialId'] == result['credentialId']
    assert active == call('eap:activate', 'eap.certificate.activate.request', activation)
    authenticate(True)
    if previous_certificate:
        lab.write(issued / 'tls-old.crt', previous_certificate)
        lab.write(issued / 'old.conf', (issued / 'eapol-test.conf').read_text().replace('/tls.crt', '/tls-old.crt'))
        authenticate(False, 'old.conf')
    eap_context = ssl.create_default_context(cafile=str(issued / 'ca.crt'))
    eap_context.load_cert_chain(str(issued / 'tls.crt'), str(issued / 'tls.key'))
    try:
        lab.request('GET', runtime_path + '/configuration', context=eap_context, origin=origin)
    except RuntimeError as error:
        assert 'HTTP 401' in str(error) or 'HTTP 403' in str(error)
    except urllib.error.URLError as error:
        assert isinstance(error.reason, ssl.SSLError) and 'ALERT' in str(error.reason).upper()
    except ssl.SSLError as error:
        assert 'ALERT_UNKNOWN_CA' in str(error)
    else:
        raise AssertionError('EAP-only certificate authenticated to network-control')
    lab.write(lab.STATE / 'eap-issuer-verification.json', {
        'verifiedAt': datetime.now(timezone.utc).isoformat(), 'credentialId': result['credentialId'],
        'fingerprint': result['fingerprint'], 'expiresAt': result['expiresAt'],
        'idempotentIssuance': 'passed', 'pendingCannotAuthenticate': 'passed',
        'activationFingerprintBinding': 'passed', 'idempotentActivation': 'passed', 'dedicatedKeyAndIdentity': 'passed',
        'issuedCertificateEAPTLS': 'passed', 'eapCannotControlHeartbeat': 'passed',
        'previousCertificateRejectedAfterRotation': 'passed' if previous_certificate else 'not exercised',
        'inputs': 'Actual CSR, mTLS API, FreeRADIUS and eapol_test; no native OS installation or physical NAS acceptance',
    })
    print('Dedicated EAP issuance, activation, identity isolation and actual EAP-TLS passed')


if __name__ == '__main__':
    try:
        main()
    except subprocess.CalledProcessError as error:
        raise SystemExit('Lab command failed with exit code ' + str(error.returncode) + '; raw output was suppressed') from None
