import { describe, expect, it } from 'vitest'
import { buildServiceDiagnosticCommand } from './diagnostics-model'

describe('buildServiceDiagnosticCommand', () => {
  it('builds bounded DNS and port probes with shell-quoted inputs', () => {
    const command = buildServiceDiagnosticCommand({
      serviceName: "api'; touch /tmp/pwn; echo '",
      namespace: 'team-a',
      port: 8080,
    })

    expect(command).toContain('getent hosts')
    expect(command).toContain('nc -vz -w 5')
    expect(command).toContain("'api'\"'\"'; touch /tmp/pwn; echo '\"'\"'.team-a.svc'")
  })
})
