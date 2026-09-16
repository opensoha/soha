"""Disposable HTTP target and DNS forwarder, reachable only inside the lab network."""
import http.server
import os
import subprocess
import socket
import socketserver
import struct
import threading


class HTTP(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        body = b'Soha VPN lab: real private resource reached\n'
        self.send_response(200)
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)


class DNS(socketserver.BaseRequestHandler):
    def handle(self):
        data, listener = self.request
        if len(data) < 12:
            return
        try:
            offset, labels = 12, []
            while data[offset]:
                length = data[offset]
                if length > 63:
                    return
                labels.append(data[offset+1:offset+1+length].decode('ascii'))
                offset += length+1
            offset += 1
            kind, cls = struct.unpack('!HH', data[offset:offset+4])
            if '.'.join(labels).lower() == 'vpn-lab.soha.test':
                question = data[12:offset+4]
                count = int(kind == 1 and cls == 1)
                response = data[:2] + struct.pack('!HHHHH', 0x8180, 1, count, 0, 0) + question
                if count:
                    response += b'\xc0\x0c' + struct.pack('!HHIH', 1, 1, 1, 4) + socket.inet_aton('10.252.250.10')
            else:
                with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as upstream:
                    upstream.settimeout(2)
                    upstream.sendto(data, ('127.0.0.11', 53))
                    response = upstream.recv(4096)
            listener.sendto(response, self.client_address)
        except (IndexError, UnicodeError, struct.error, OSError):
            return


subprocess.run(['/sbin/ip', 'addr', 'add', os.environ['LAB_RESOURCE_IP'] + '/32', 'dev', 'lo'], check=True)
threading.Thread(target=socketserver.ThreadingUDPServer(('0.0.0.0', 53), DNS).serve_forever, daemon=True).start()
http.server.ThreadingHTTPServer(('0.0.0.0', 8000), HTTP).serve_forever()
