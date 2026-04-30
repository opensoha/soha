const DEV_API_HOST = '127.0.0.1:8080'

export function resolveApiWebSocketHost() {
  return process.env.NODE_ENV === 'development' ? DEV_API_HOST : window.location.host
}
