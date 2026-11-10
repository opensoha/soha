#!/usr/bin/env python3
"""Safety boundaries for opt-in, owned remote image verification."""
import copy
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import local_verify_remote as remote


class RemoteVerificationTests(unittest.TestCase):
    def test_endpoint_and_environment_cannot_retarget_or_bypass_guard(self):
        with patch.dict(os.environ, {}, clear=True):
            remote.validate_endpoint('ssh://database')
            for endpoint in ('ssh://other', 'tcp://10.0.0.18:2375', 'unix:///tmp/docker.sock'):
                with self.subTest(endpoint=endpoint), self.assertRaises(RuntimeError):
                    remote.validate_endpoint(endpoint)
        for key, value in [('DOCKER_HOST', 'ssh://other'), ('DOCKER_CONTEXT', 'other'),
                           ('BUILDX_BUILDER', 'shared'), ('CI', 'true'),
                           ('GITHUB_ACTIONS', '1'), ('OPENSOHA_VERIFY_RELEASE', 'yes')]:
            with self.subTest(key=key), patch.dict(os.environ, {key: value}, clear=True), self.assertRaises(RuntimeError):
                remote.validate_endpoint(remote.ENDPOINT)

    def test_capacity_checks_actual_docker_root_without_a_host_mount(self):
        info = {'ID': 'engine', 'Name': 'owned-endpoint', 'DockerRootDir': '/var/lib/docker'}
        outputs = [json.dumps(info), 'Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/test 30000000 1000000 22000000 4% /\n']
        with patch.object(remote, 'command', side_effect=outputs) as call:
            receipt = remote.remote_capacity(['docker', '--host', remote.ENDPOINT], 10 * remote.GIB)
        self.assertEqual(receipt['freeBytes'], 22000000 * 1024)
        self.assertEqual(call.call_args_list[1].args[0], ['ssh', '-o', 'BatchMode=yes', 'database', 'df', '-Pk', '--', '/var/lib/docker'])
        for root in ('/var/lib/docker;id', '/var/../etc', ''):
            with patch.object(remote, 'command', return_value=json.dumps(dict(info, DockerRootDir=root))), self.assertRaises(RuntimeError):
                remote.remote_capacity(['docker'], remote.GIB)
        with patch.object(remote, 'command', side_effect=[json.dumps(info), 'Filesystem x\n/dev/a 100 99 1 99% /\n']), self.assertRaises(RuntimeError):
            remote.remote_capacity(['docker'], remote.GIB)

    def fixture(self):
        report = {}
        builder = remote.RemoteBuilder(remote.ENDPOINT, 'a' * 32, '/owned', report)
        container = {'Id': 'full-original-container-id', 'Name': '/' + builder.container, 'Image': 'sha256:original',
            'Mounts': [{'Type': 'volume', 'Name': builder.volume, 'Destination': '/var/lib/buildkit', 'RW': True}]}
        volume = {'Name': builder.volume, 'Driver': 'local', 'Mountpoint': '/var/lib/docker/volumes/owned/_data',
                  'CreatedAt': 'original', 'Labels': {'com.docker.volume.anonymous': ''}, 'Options': None}
        metadata = {'Name': builder.name, 'Driver': 'docker-container', 'Nodes': [{'Name': builder.node, 'Endpoint': remote.ENDPOINT}]}
        return builder, container, volume, metadata

    def test_capture_and_cleanup_refuse_rebound_container_volume_or_endpoint(self):
        for scenario in ('container', 'volume', 'endpoint', 'host-mount', 'missing'):
            builder, container, volume, metadata = self.fixture()
            with patch.object(remote, 'command', return_value=json.dumps(metadata)), patch.object(remote, 'inspect', side_effect=[container, volume]):
                builder.capture()
            original = copy.deepcopy(builder.identity)
            changed_container, changed_volume, changed_metadata = copy.deepcopy(container), copy.deepcopy(volume), copy.deepcopy(metadata)
            if scenario == 'container':
                changed_container['Id'] = 'replacement'
            elif scenario == 'volume':
                changed_volume['CreatedAt'] = 'replacement'
            elif scenario == 'endpoint':
                changed_metadata['Nodes'][0]['Endpoint'] = 'ssh://other'
            elif scenario == 'host-mount':
                changed_container['Mounts'][0]['Type'] = 'bind'
            else:
                changed_container = None
            builder.attempted, builder.created, builder.bootstrap_attempted = True, True, True
            with self.subTest(scenario=scenario), patch.object(remote, 'command', return_value=json.dumps(changed_metadata)) as call, \
                    patch.object(remote, 'inspect', side_effect=lambda _docker, kind, _name: changed_container if kind == 'container' else changed_volume), self.assertRaises(RuntimeError):
                builder.close()
            self.assertEqual(builder.identity, original)
            self.assertFalse(any('rm' in entry.args[0] for entry in call.call_args_list))

    def test_preexisting_random_name_never_creates_or_removes_resources(self):
        builder, _, _, _ = self.fixture()
        with patch.object(remote, 'command', return_value=json.dumps({'Name': builder.name})) as call, self.assertRaises(RuntimeError):
            builder.start()
        self.assertFalse(builder.created)
        self.assertFalse(any('create' in entry.args[0] or 'rm' in entry.args[0] for entry in call.call_args_list))

    def test_owned_active_cache_never_pruned(self):
        builder, _, _, _ = self.fixture()
        with patch.object(builder, 'capture'), patch.object(remote, 'command', return_value=json.dumps({'Reclaimable': False, 'Size': 1})) as call, self.assertRaises(RuntimeError):
            builder.cache()
        self.assertFalse(any('prune' in entry.args[0] for entry in call.call_args_list))

    def test_missing_resource_requires_successful_listing_and_transport_failure_is_not_absence(self):
        with patch.object(remote, 'command', return_value=''):
            self.assertIsNone(remote.inspect(['docker'], 'container', 'owned'))
        with patch.object(remote, 'command', side_effect=RuntimeError('transport unavailable')), self.assertRaises(RuntimeError):
            remote.inspect(['docker'], 'container', 'owned')

    def test_build_inputs_cannot_export_paths_outside_owned_snapshots(self):
        with tempfile.TemporaryDirectory() as directory, tempfile.TemporaryDirectory() as outside:
            root = Path(directory)
            (root / 'Dockerfile').write_text('FROM scratch\n')
            outside_file = Path(outside) / 'Dockerfile'
            outside_file.write_text('FROM scratch\n')
            (root / 'contracts').mkdir()
            valid = ['--build-context', 'contracts=' + str(root / 'contracts'), '-f', str(root / 'Dockerfile'), str(root)]
            with patch.object(Path, 'cwd', return_value=root):
                remote.validate_build_inputs(valid, root)
                remote.validate_build_inputs(['--quiet', '--file=' + str(root / 'Dockerfile'), str(root)], root)
                remote.validate_build_inputs(['-f=' + str(root / 'Dockerfile'), str(root)], root)
                for build in ([*valid[:-1], outside], ['--build-context=contracts=' + outside, str(root)],
                              ['--build-context=other=' + str(root), str(root)], ['--file=' + outside, str(root)],
                              ['-f' + str(outside_file), str(root)], ['-qf' + str(outside_file), str(root)],
                              ['-qo' + outside, str(root)], ['-qtunattested:image', str(root)]):
                    with self.subTest(build=build), self.assertRaises(RuntimeError):
                        remote.validate_build_inputs(build, root)
                (root / 'escape').symlink_to(outside, target_is_directory=True)
                with self.assertRaises(RuntimeError):
                    remote.validate_build_inputs(['--build-context=contracts=' + str(root / 'escape'), str(root)], root)

    def test_lost_create_ack_reconciles_only_original_local_builder_metadata(self):
        builder, _, _, metadata = self.fixture()
        builder.attempted = True
        outputs = [json.dumps({'Name': builder.name}), json.dumps(metadata), '', '']
        with patch.object(remote, 'command', side_effect=outputs) as call, patch.object(remote, 'inspect', return_value=None):
            builder.close()
        self.assertEqual(builder.report['builderCleanup'], 'PASS')
        self.assertEqual(call.call_args_list[2].args[0][-3:], ['buildx', 'rm', builder.name])

    def test_partial_bootstrap_retains_receipted_resources_without_removal(self):
        builder, _, volume, metadata = self.fixture()
        builder.attempted, builder.created, builder.bootstrap_attempted = True, True, True
        with patch.object(remote, 'command', side_effect=[json.dumps({'Name': builder.name}), json.dumps(metadata)]) as call, \
                patch.object(remote, 'inspect', side_effect=[None, volume]), self.assertRaises(RuntimeError):
            builder.close()
        self.assertEqual(builder.report['cleanupInventory']['volumeName'], builder.volume)
        self.assertFalse(any('rm' in entry.args[0] for entry in call.call_args_list))

    def test_remote_image_cleanup_needs_exact_iid_label_and_no_container(self):
        with tempfile.TemporaryDirectory() as directory:
            iid = Path(directory) / 'image.id'
            iid.write_text('sha256:' + 'a' * 64)
            image = {'Id': iid.read_text(), 'Config': {'Labels': {'owner': 'run'}}}
            for scenario in ('changed-id', 'changed-label', 'container'):
                value = copy.deepcopy(image)
                if scenario == 'changed-id': value['Id'] = 'sha256:' + 'b' * 64
                if scenario == 'changed-label': value['Config']['Labels']['owner'] = 'other'
                outputs = ['image exists', json.dumps([value]), 'referenced-container']
                with self.subTest(scenario=scenario), patch.object(remote, 'command', side_effect=outputs) as call, self.assertRaises(RuntimeError):
                    remote.cleanup_image(['docker'], 'owned:test', iid, 'owner', 'run')
                self.assertFalse(any('rm' in entry.args[0] for entry in call.call_args_list))

    def test_formatted_cache_sizes_do_not_prevent_owned_volume_removal(self):
        builder, container, volume, metadata = self.fixture()
        builder.attempted = builder.created = builder.bootstrap_attempted = True
        record = json.dumps({'Reclaimable': True, 'Size': '4.096kB'})
        outputs = [json.dumps({'Name': builder.name}), record, '', record, '', '']
        with patch.object(builder, 'builder', return_value=metadata), patch.object(builder, 'capture'), \
                patch.object(remote, 'inspect', side_effect=[container, volume, None, None]), \
                patch.object(remote, 'command', side_effect=outputs) as call:
            builder.close()
        self.assertEqual(builder.report['builderCleanup'], 'PASS')
        self.assertEqual(builder.report['cacheBeforeRemovalReportedSizes'], ['4.096kB'])
        self.assertEqual(builder.report['cacheAfterBytes'], 0)
        self.assertEqual(sum('rm' in entry.args[0] for entry in call.call_args_list), 1)

    def test_cache_becoming_active_after_prune_is_retained(self):
        builder, _, _, _ = self.fixture()
        with patch.object(builder, 'capture'), patch.object(remote, 'command', side_effect=[
                json.dumps({'Reclaimable': True, 'Size': '4.096kB'}), '',
                json.dumps({'Reclaimable': False, 'Size': '4.096kB'})]), self.assertRaises(RuntimeError):
            builder.cache()
        with patch.object(remote, 'command', side_effect=[json.dumps({'Names': 'owned'}), RuntimeError('inspect unavailable')]), self.assertRaises(RuntimeError):
            remote.inspect(['docker'], 'container', 'owned')


if __name__ == '__main__':
    unittest.main()
