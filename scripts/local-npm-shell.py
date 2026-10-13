#!/usr/bin/env python3
"""Apply the installed local verification guard to OpenSoha npm scripts."""

import os
from pathlib import Path
import sys


WORKSPACE = Path(__file__).resolve().parents[2]


def guarded(cwd, event, command):
    path = Path(cwd).resolve()
    if not path.is_relative_to(WORKSPACE) or 'node_modules' in path.parts:
        return False
    if event in ('dev', 'start', 'serve', 'preview', 'test:watch') or '--watch' in command:
        return False
    return (event.split(':')[0] in ('build', 'test', 'lint', 'typecheck', 'check',
                                    'verify', 'validate', 'generate', 'smoke')
            or event in ('format:check', 'browser:baseline')
            or event.startswith(('api:reference:', 'screenshots:', 'package:', 'release:')))


def main():
    command = ['/bin/sh', *sys.argv[1:]]
    if not any(os.environ.get(key, '').lower() in ('1', 'true', 'yes') for key in
               ('CI', 'GITHUB_ACTIONS', 'OPENSOHA_VERIFY_RELEASE', 'OPENSOHA_VERIFY_INTERNAL',
                'OPENSOHA_VERIFY_PASSTHROUGH')) and guarded(
            os.getcwd(), os.environ.get('npm_lifecycle_event', ''),
            os.environ.get('npm_lifecycle_script', '')):
        command = [str(Path(__file__).resolve().with_name('local-verify.py')), 'run', '--', *command]
    os.execvpe(command[0], command, os.environ)


if __name__ == '__main__':
    main()
