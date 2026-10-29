#!/usr/bin/env python3
"""Exercise the owned software lab; heartbeat and Accounting inputs are synthetic."""

from datetime import datetime, timedelta, timezone
import importlib.util
import json
from pathlib import Path
import re
import ssl
import subprocess
import time
import uuid

spec = importlib.util.spec_from_file_location('radius_lab', Path(__file__).with_name('radius-lab.py'))
lab = importlib.util.module_from_spec(spec)
spec.loader.exec_module(lab)


def items(result):
    return result.get('items', []) if isinstance(result, dict) else result


def main():
    lab.owned_state()
    seed = json.loads((lab.STATE / 'seed.json').read_text())
    token = lab.admin_token()
    upgrade = lab.STATE / 'endpoint-presence-enrollment.json'
    if not upgrade.exists():
        folder = lab.STATE / 'tls' / 'eap-client'
        backup = lab.STATE / 'tls' / 'eap-client-before-presence'
        if not backup.exists():
            folder.rename(backup)
        lab.certificate('eap-client', seed['subjectId'],
                        uri='spiffe://opensoha.local/network-control/endpoint/' + lab.DEVICE_ID)
        lab.write(folder / 'eapol-test.conf', (backup / 'eapol-test.conf').read_text())
        lab.enroll(token, lab.DEVICE_ID, 'endpoint', seed['subjectId'], lab.DEVICE_ID, 'eap-client',
                   ['radius-eap-tls-v1', 'endpoint-presence-v1'])
        lab.write(upgrade, {'runtimeId': lab.DEVICE_ID, 'upgradedAt': datetime.now(timezone.utc).isoformat()})
    folder = lab.STATE / 'tls' / 'eap-client'
    context = ssl.create_default_context(cafile=str(folder / 'ca.crt'))
    context.load_cert_chain(str(folder / 'tls.crt'), str(folder / 'tls.key'))
    boot, sequence, admission_version = str(uuid.uuid4()), 0, 0

    def presence(status):
        nonlocal sequence, admission_version
        sequence += 1
        now = datetime.now(timezone.utc)
        message = {'schemaVersion': 'network-runtime/v1alpha1', 'messageId': str(uuid.uuid4()),
                   'messageType': 'endpoint.presence.request', 'producerId': lab.DEVICE_ID,
                   'runtimeId': lab.DEVICE_ID, 'runtimeKind': 'endpoint', 'occurredAt': now.isoformat(),
                   'expiresAt': (now + timedelta(minutes=1)).isoformat(),
                   'payload': {'deviceId': lab.DEVICE_ID, 'bootId': boot, 'sequence': sequence,
                               'status': status, 'uiState': 'online', 'uiObservedAt': now.isoformat(),
                               'configurationVersion': 0, 'policyVersion': 0, 'admissionPolicyVersion': admission_version}}
        result = lab.request('POST', '/runtimes/' + lab.DEVICE_ID + '/endpoint:presence', body=message,
                             context=context, origin='https://127.0.0.1:18182/api/network-control/v1')
        assert result['payload']['accepted']
        admission_version = result['payload']['admissionPolicyVersion']
        assert admission_version > 0

    def sessions():
        return items(lab.request('GET', '/network-access/sessions?deviceId=' + lab.DEVICE_ID + '&siteId=' + seed['siteId'], token))

    def authenticate(profile):
        before = {row['id'] for row in sessions()}
        output = lab.compose('run', '--rm', '--no-deps', 'eap-client')
        assert re.search(r'^SUCCESS$', output, re.MULTILINE)
        new = [row for row in sessions() if row['id'] not in before and row['nasId'] == lab.NAS_ID]
        assert len(new) == 1 and new[0]['accessProfile'] == profile
        return new[0]

    def accounting(session, status):
        assert re.fullmatch(r'[A-Za-z0-9._:-]+', session['id'])
        attrs = {'Acct-Status-Type': status, 'Acct-Session-Id': 'presence-' + session['id'], 'Class': session['id'],
                 'NAS-Identifier': lab.NAS_ID, 'NAS-Port-Id': 'software-port-1', 'User-Name': seed['subjectId'],
                 'Calling-Station-Id': '02:00:00:00:00:01', 'Event-Timestamp': int(time.time()),
                 'Acct-Session-Time': max(1, int(time.monotonic() - started)), 'Message-Authenticator': '0x00'}
        packet = '\n'.join(key + ' = ' + (str(value) if isinstance(value, int) or key in ('Acct-Status-Type', 'Message-Authenticator') else '"' + value + '"') for key, value in attrs.items()) + '\n'
        reply = lab.compose('run', '--rm', '--no-deps', '-T', '--entrypoint', '/usr/bin/radclient', 'radius-client',
                            '-x', '-r', '1', '-t', '5', '-S', '/lab/secrets/shared-secret', '127.0.0.1:1813', 'acct', input=packet)
        assert 'Accounting-Response' in reply

    presence('healthy')
    current = lab.request('GET', '/network-access/endpoint-devices/' + lab.DEVICE_ID + '/presence', token)
    assert current['serviceState'] == 'healthy' and current['uiState'] == 'unknown'
    session = authenticate('full')
    started = time.monotonic()
    accounting(session, 'Start')
    configuration = {'name': 'Task53 isolated endpoint response ' + boot, 'enabled': True,
                     'siteIds': [seed['siteId']], 'nasBindingIds': [], 'deviceIds': [lab.DEVICE_ID],
                     'subjects': {'users': [seed['subjectId']], 'teams': [], 'tags': []},
                     'signal': 'service_unavailable', 'requireHealthyForAdmission': True,
                     'heartbeatIntervalSeconds': 30, 'missedHeartbeats': 1, 'gracePeriodSeconds': 0,
                     'accountingFreshnessSeconds': 120, 'restoreHeartbeats': 2, 'restoreObservationSeconds': 30,
                     'autoRestore': False, 'steps': [{'afterSeconds': 0, 'action': 'restrict', 'targetAccessProfile': 'restricted'}],
                     'failureAction': 'record'}
    policy = lab.request('POST', '/network-access/endpoint-response-policies', token,
                         {'expectedRevision': 0, 'configuration': configuration})
    policy_path = '/network-access/endpoint-response-policies/' + policy['id']
    lab.request('POST', policy_path + '/publish', token, {'expectedRevision': policy['revision']})
    preview = lab.request('POST', policy_path + '/preview', token, {'deviceId': lab.DEVICE_ID, 'sessionId': session['id']})
    assert not preview['matched'] and len(preview['sessions']) == 1 and 'restrict' in preview['unsupportedActions']
    presence('unhealthy')
    current = lab.request('GET', '/network-access/endpoint-devices/' + lab.DEVICE_ID + '/presence', token)
    assert current['serviceState'] == 'degraded' and current['policyVersion'] == admission_version
    event = None
    for _ in range(30):
        rows = items(lab.request('GET', '/network-access/endpoint-response-events?policyId=' + policy['id'], token))
        event = next((row for row in rows if row['sessionId'] == session['id'] and row['status'] == 'enforcement_unknown'), None)
        if event:
            break
        time.sleep(2)
    assert event and event['restrictionEpoch'] > 0 and not event['commandIds']
    detected_seconds = time.monotonic() - started
    try:
        lab.compose('run', '--rm', '--no-deps', 'eap-client')
    except subprocess.CalledProcessError as error:
        assert 'Access-Reject' in error.stdout
    else:
        raise AssertionError('An unhealthy endpoint bypassed admission protection')
    presence('healthy')
    time.sleep(32)
    presence('healthy')
    accounting(session, 'Interim-Update')
    authenticate('restricted')
    event_path = '/network-access/endpoint-response-events/' + event['id']
    event = lab.request('GET', event_path, token)
    try:
        lab.request('POST', event_path + '/restore', token, {'expectedRevision': event['revision'], 'reason': 'Synthetic health recovered; NAS still online'})
    except RuntimeError as error:
        assert 'HTTP 409' in str(error)
    else:
        raise AssertionError('Unsupported NAS enforcement restored without old-session Stop')
    accounting(session, 'Stop')
    for attempt in range(3):
        event = lab.request('GET', event_path, token)
        try:
            restored = lab.request('POST', event_path + '/restore', token,
                                   {'expectedRevision': event['revision'], 'reason': 'Strongly bound synthetic NAS Stop and current continuous health'})
            break
        except RuntimeError as error:
            if 'HTTP 409' not in str(error) or attempt == 2:
                raise
    assert restored['status'] == 'restored'
    assert next(row for row in sessions() if row['id'] == session['id'])['status'] in ('expired', 'revoked')
    authenticate('full')
    policy = lab.request('GET', policy_path, token)
    disabled = lab.request('PUT', policy_path, token, {'expectedRevision': policy['revision'], 'configuration': {**configuration, 'enabled': False}})
    lab.request('POST', policy_path + '/publish', token, {'expectedRevision': disabled['revision']})
    lab.write(lab.STATE / 'endpoint-response-verification.json', {
        'verifiedAt': datetime.now(timezone.utc).isoformat(), 'policyId': policy['id'], 'eventId': event['id'],
        'sessionId': session['id'], 'detectedSeconds': round(detected_seconds, 3),
        'uiCapabilityGuard': 'passed', 'separateAdmissionVersion': 'passed', 'unhealthyAdmissionDenied': 'passed',
        'unsupportedActionReported': 'passed', 'reconnectFloor': 'passed', 'restoreRequiresNASStop': 'passed',
        'oldSessionInvalidAfterRestore': 'passed', 'freshAuthenticationAfterRestore': 'passed',
        'inputs': 'synthetic heartbeat and Accounting over actual mTLS/RADIUS; no OS or physical NAS acceptance',
    })
    print('Endpoint presence, persisted restrictions, reconnect floor and guarded recovery passed in the software lab')


if __name__ == '__main__':
    try:
        main()
    except subprocess.CalledProcessError as error:
        raise SystemExit('Lab command failed with exit code ' + str(error.returncode) + '; raw output was suppressed') from None
