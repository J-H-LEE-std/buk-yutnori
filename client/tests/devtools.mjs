import { randomBytes } from 'node:crypto';
import net from 'node:net';
function encodeClientFrame(text, opcode = 0x1) {
  const payload = Buffer.isBuffer(text) ? text : Buffer.from(text);
  const mask = randomBytes(4);
  let header;
  if (payload.length < 126) {
    header = Buffer.from([0x80 | opcode, 0x80 | payload.length]);
  } else if (payload.length <= 0xffff) {
    header = Buffer.alloc(4);
    header[0] = 0x80 | opcode;
    header[1] = 0x80 | 126;
    header.writeUInt16BE(payload.length, 2);
  } else {
    header = Buffer.alloc(10);
    header[0] = 0x80 | opcode;
    header[1] = 0x80 | 127;
    header.writeBigUInt64BE(BigInt(payload.length), 2);
  }
  const maskedPayload = Buffer.alloc(payload.length);
  for (let index = 0; index < payload.length; index += 1) {
    maskedPayload[index] = payload[index] ^ mask[index % mask.length];
  }
  return Buffer.concat([header, mask, maskedPayload]);
}

class DevToolsSocket {
  constructor(socket, initialData) {
    this.socket = socket;
    this.buffer = initialData;
    this.fragment = Buffer.alloc(0);
    this.onMessage = () => {};
    socket.on("data", (chunk) => {
      this.buffer = Buffer.concat([this.buffer, chunk]);
      this.processFrames();
    });
    if (this.buffer.length > 0) this.processFrames();
  }

  processFrames() {
    while (this.buffer.length >= 2) {
      const first = this.buffer[0];
      const second = this.buffer[1];
      const final = (first & 0x80) !== 0;
      const opcode = first & 0x0f;
      const masked = (second & 0x80) !== 0;
      let payloadLength = second & 0x7f;
      let offset = 2;
      if (payloadLength === 126) {
        if (this.buffer.length < 4) return;
        payloadLength = this.buffer.readUInt16BE(2);
        offset = 4;
      } else if (payloadLength === 127) {
        if (this.buffer.length < 10) return;
        payloadLength = Number(this.buffer.readBigUInt64BE(2));
        offset = 10;
      }
      const maskLength = masked ? 4 : 0;
      if (this.buffer.length < offset + maskLength + payloadLength) return;
      const mask = masked ? this.buffer.subarray(offset, offset + 4) : null;
      offset += maskLength;
      const payload = Buffer.from(this.buffer.subarray(offset, offset + payloadLength));
      this.buffer = this.buffer.subarray(offset + payloadLength);
      if (mask) {
        for (let index = 0; index < payload.length; index += 1) {
          payload[index] ^= mask[index % mask.length];
        }
      }

      if (opcode === 0x8) {
        this.socket.end();
      } else if (opcode === 0x9) {
        this.socket.write(encodeClientFrame(payload, 0xa));
      } else if (opcode === 0x1 || opcode === 0x0) {
        this.fragment = Buffer.concat([this.fragment, payload]);
        if (final) {
          this.onMessage(this.fragment.toString("utf8"));
          this.fragment = Buffer.alloc(0);
        }
      }
    }
  }

  send(text) {
    this.socket.write(encodeClientFrame(text));
  }

  close() {
    this.socket.write(encodeClientFrame("", 0x8));
    this.socket.end();
  }
}

export function connect(webSocketDebuggerURL) {
  const url = new URL(webSocketDebuggerURL);
  return new Promise((resolve, reject) => {
    const socket = net.createConnection(Number(url.port), url.hostname);
    const key = randomBytes(16).toString("base64");
    let response = Buffer.alloc(0);
    const fail = (error) => reject(new Error(`DevTools WebSocket connection failed: ${error.message}`));
    socket.once("error", fail);
    socket.on("connect", () => {
      socket.write([
        `GET ${url.pathname}${url.search} HTTP/1.1`,
        `Host: ${url.host}`,
        "Upgrade: websocket",
        "Connection: Upgrade",
        `Sec-WebSocket-Key: ${key}`,
        "Sec-WebSocket-Version: 13",
        "\r\n",
      ].join("\r\n"));
    });
    const receiveHandshake = (chunk) => {
      response = Buffer.concat([response, chunk]);
      const headerEnd = response.indexOf("\r\n\r\n");
      if (headerEnd < 0) return;
      socket.off("data", receiveHandshake);
      socket.off("error", fail);
      const header = response.subarray(0, headerEnd).toString("utf8");
      if (!header.startsWith("HTTP/1.1 101")) {
        reject(new Error(`DevTools WebSocket upgrade failed: ${header.split("\r\n")[0]}`));
        socket.end();
        return;
      }
      resolve(new DevToolsSocket(socket, response.subarray(headerEnd + 4)));
    };
    socket.on("data", receiveHandshake);
  });
}
