function shellQuote(value: string) {
  return `'${value.replace(/'/g, `'"'"'`)}'`
}

export function buildServiceDiagnosticCommand({
  namespace,
  port,
  serviceName,
}: {
  namespace: string
  port: number
  serviceName: string
}) {
  const host = shellQuote(`${serviceName}.${namespace}.svc`)
  return [
    `echo 'DNS'; if command -v getent >/dev/null 2>&1; then getent hosts ${host}; elif command -v nslookup >/dev/null 2>&1; then nslookup ${host}; else echo 'No DNS diagnostic tool found' >&2; exit 127; fi`,
    `echo 'PORT'; if command -v nc >/dev/null 2>&1; then nc -vz -w 5 ${host} ${port}; elif command -v curl >/dev/null 2>&1; then curl -fsS --connect-timeout 5 -o /dev/null http://${host}:${port}/; elif command -v wget >/dev/null 2>&1; then wget -q -T 5 -O /dev/null http://${host}:${port}/; else echo 'No port diagnostic tool found' >&2; exit 127; fi`,
  ].join('; ')
}
