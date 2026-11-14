import argparse
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location('radius_lab', Path(__file__).with_name('radius-lab.py'))
lab = importlib.util.module_from_spec(spec)
spec.loader.exec_module(lab)


class RadiusLabTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.previous_state = lab.STATE
        lab.configure(argparse.Namespace(legacy_task53=False, project='task122-test', owner='task122-b3-l02-a01',
                      state=str(Path(self.temp.name).resolve() / 'lab'), core_image='lab/core:test', radius_image='lab/radius:test',
                      eap_image='lab/eap:test', api='http://127.0.0.1:29180/api/v1', runtime_id='radius-task122',
                      nas_id='nas-task122', device_id='device-task122', api_port=29180, control_port=29182, ingest_port=29183))
        lab.STATE = Path(self.temp.name).resolve() / 'lab'

    def tearDown(self):
        lab.STATE = self.previous_state
        self.temp.cleanup()

    def test_configuration_marker_rejects_changed_owner_or_port(self):
        lab.owned_state(create=True)
        lab.OWNER = 'someone-else'
        with self.assertRaises(RuntimeError):
            lab.owned_state()
        self.assertEqual(json.loads((lab.STATE / 'lab.json').read_text())['owner'], 'task122-b3-l02-a01')

    def test_compose_refuses_foreign_container_before_mutation(self):
        lab.prepare()
        def command(*args, **kwargs):
            if args[:3] == ('docker', 'ps', '-aq'):
                return 'foreign-container'
            if args[:3] == ('docker', 'container', 'inspect'):
                return json.dumps([{'Config': {'Labels': {'io.opensoha.lab.owner': 'other'}}}])
            self.fail('Unexpected external command: ' + str(args[:3]))
        with patch.object(lab, 'run', side_effect=command):
            with self.assertRaises(RuntimeError):
                lab.compose('stop')

    def test_request_uses_current_configured_api(self):
        with patch.object(lab.urllib.request, 'urlopen') as call:
            call.return_value.__enter__.return_value.read.return_value = b'{}'
            lab.request('GET', '/test')
        self.assertEqual(call.call_args.args[0].full_url, lab.API + '/test')

    def test_refuses_unowned_existing_directory(self):
        lab.STATE.mkdir()
        (lab.STATE / 'keep.txt').write_text('existing data')
        with self.assertRaises(RuntimeError):
            lab.prepare()
        self.assertEqual((lab.STATE / 'keep.txt').read_text(), 'existing data')

    def test_prepare_preserves_credentials_and_isolates_every_mount(self):
        lab.prepare()
        credentials = (lab.STATE / 'secrets.json').read_text()
        ca = (lab.STATE / 'tls' / 'ca.crt').read_text()
        lab.prepare()
        self.assertEqual((lab.STATE / 'secrets.json').read_text(), credentials)
        self.assertEqual((lab.STATE / 'tls' / 'ca.crt').read_text(), ca)
        config = json.loads((lab.STATE / 'compose.json').read_text())
        self.assertEqual(config['name'], 'task122-test')
        ports = []
        for service in config['services'].values():
            self.assertNotIn('container_name', service)
            self.assertNotIn('privileged', service)
            self.assertNotEqual(service.get('network_mode'), 'host')
            for port in service.get('ports', []):
                self.assertTrue(port.startswith('127.0.0.1:'))
                ports.append(port.split(':')[1])
            for mount in service.get('volumes', []):
                source = mount.split(':')[0]
                if source.startswith('/'):
                    self.assertTrue(source.startswith(str(lab.STATE) + '/'))
                    self.assertNotEqual(source, str(lab.STATE / 'tls'))
        self.assertEqual(len(ports), len(set(ports)))
        self.assertEqual(set(config['volumes']), {'postgres-data', 'ingest-postgres-data', 'core-data'})
        for volume in config['volumes'].values():
            self.assertEqual(volume['labels']['io.opensoha.lab.owner'], lab.OWNER)
        for key in (lab.STATE / 'tls').glob('**/*.key'):
            self.assertEqual(key.stat().st_mode & 0o777, 0o600)
        certificate = lab.run('openssl', 'x509', '-in', str(lab.STATE / 'tls' / 'freeradius-ingest' / 'tls.crt'), '-noout', '-text')
        self.assertIn('spiffe://opensoha.local/network-ingest/freeradius/radius-task122', certificate)
        self.assertNotIn('/ca.key:', json.dumps(config))
        issuer = lab.STATE / 'tls' / 'eap-issuer' / 'ca.crt'
        self.assertNotEqual(issuer.read_text(), ca)
        self.assertIn(issuer.read_text(), (lab.STATE / 'tls' / 'freeradius' / 'client-ca.crt').read_text())
        issuer_mount = str(lab.STATE / 'tls' / 'eap-issuer')
        for name, service in config['services'].items():
            mounts_issuer = any(mount.startswith(issuer_mount + ':') for mount in service.get('volumes', []))
            self.assertEqual(mounts_issuer, name == 'network-control')

    def test_seed_enrolls_before_binding_and_resumes_without_duplicate_objects(self):
        lab.prepare()
        created = []
        enrolled = []

        def request(method, path, token=None, body=None):
            if path == '/auth/me':
                return {'userId': 'subject-test'}
            if path.endswith('/registration'):
                return {'id': lab.DEVICE_ID, 'name': 'test client'}
            if path == '/network-access/policies/compile':
                return {'policyVersion': 1}
            if method == 'POST':
                if path == '/network-access/nas-bindings':
                    self.assertEqual(enrolled.count('nas'), 1)
                created.append(path)
                return {'id': 'object-' + str(len(created))}
            return {}

        def enroll(*args):
            kind = args[2]
            enrolled.append(kind)
            if kind == 'endpoint' and enrolled.count(kind) == 1:
                raise RuntimeError('temporary endpoint enrollment failure')

        with patch.object(lab, 'admin_token', return_value='test-token'), patch.object(lab, 'request', side_effect=request), \
                patch.object(lab, 'enroll', side_effect=enroll), patch.object(lab.time, 'sleep'):
            with self.assertRaises(RuntimeError):
                lab.seed()
            self.assertFalse((lab.STATE / 'seed.json').exists())
            lab.seed()
            lab.seed()
        self.assertEqual(enrolled, ['nas', 'endpoint', 'endpoint'])
        self.assertEqual(created.count('/network-access/sites'), 1)
        self.assertEqual(created.count('/network-access/policies'), 1)
        self.assertEqual(created.count('/network-access/nas-bindings'), 1)
        self.assertEqual(json.loads((lab.STATE / 'seed.json').read_text())['deviceId'], lab.DEVICE_ID)


if __name__ == '__main__':
    unittest.main()
