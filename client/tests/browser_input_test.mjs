import { randomBytes } from "node:crypto";
import net from "node:net";

const debuggerURL = process.argv[2];
const pageURL = process.argv[3];

if (!debuggerURL || !pageURL) {
  throw new Error("usage: node browser_input_test.mjs <debugger-url> <page-url>");
}
const delay = (milliseconds) => new Promise((resolve) => {
  setTimeout(resolve, milliseconds);
});

async function findPageTarget() {
  let lastError;
  for (let attempt = 0; attempt < 100; attempt += 1) {
    try {
      const response = await fetch(`${debuggerURL}/json/list`, { cache: "no-store" });
      if (!response.ok) throw new Error(`DevTools target list returned ${response.status}`);
      const targets = await response.json();
      const target = targets.find((candidate) => (
        candidate.type === "page" && candidate.url.startsWith(pageURL)
      ));
      if (target) return target;
    } catch (error) {
      lastError = error;
    }
    await delay(100);
  }
  throw new Error(`Chrome page target did not become ready: ${lastError ?? "not found"}`);
}

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

function connect(webSocketDebuggerURL) {
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

const target = await findPageTarget();
const socket = await connect(target.webSocketDebuggerUrl);
let nextID = 1;
const pending = new Map();

socket.onMessage = (text) => {
  const message = JSON.parse(text);
  if (!message.id || !pending.has(message.id)) return;
  const { resolve, reject } = pending.get(message.id);
  pending.delete(message.id);
  if (message.error) reject(new Error(JSON.stringify(message.error)));
  else resolve(message.result);
};

function command(method, params = {}) {
  const id = nextID;
  nextID += 1;
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject });
    socket.send(JSON.stringify({ id, method, params }));
  });
}

async function evaluate(expression, awaitPromise = false) {
  const response = await command("Runtime.evaluate", {
    expression,
    awaitPromise,
    returnByValue: true,
  });
  if (response.exceptionDetails) {
    throw new Error(response.exceptionDetails.text);
  }
  return response.result.value;
}

try {
  await command("Runtime.enable");
  await evaluate(`new Promise((resolve, reject) => {
    const deadline = Date.now() + 10000;
    const waitForRuntime = () => {
      const input = document.getElementById("ime-input");
      if (input && !input.disabled && typeof Module?.ccall === "function") {
        resolve(true);
      } else if (Date.now() >= deadline) {
        reject(new Error("WASM input did not become ready"));
      } else {
        setTimeout(waitForRuntime, 50);
      }
    };
    waitForRuntime();
  })`, true);

  const initial = await evaluate(`(() => {
    const input = document.getElementById("ime-input");
    input.focus();
    input.value = "가나다";
    input.setSelectionRange(input.value.length, input.value.length);
    input.dispatchEvent(new InputEvent("input", {
      bubbles: true,
      inputType: "insertText",
      data: "가나다",
    }));
    return {
      value: input.value,
      echo: document.getElementById("echo").textContent,
    };
  })()`);
  if (initial.value !== "가나다" || initial.echo !== "가나다") {
    throw new Error(`initial DOM/WASM input synchronization failed: ${JSON.stringify(initial)}`);
  }

  await command("Input.dispatchKeyEvent", {
    type: "keyDown",
    key: "Backspace",
    code: "Backspace",
    windowsVirtualKeyCode: 8,
    nativeVirtualKeyCode: 8,
  });
  await command("Input.dispatchKeyEvent", {
    type: "keyUp",
    key: "Backspace",
    code: "Backspace",
    windowsVirtualKeyCode: 8,
    nativeVirtualKeyCode: 8,
  });
  await delay(100);

  const result = await evaluate(`(() => ({
    value: document.getElementById("ime-input").value,
    echo: document.getElementById("echo").textContent,
  }))()`);
  if (result.value !== "가나" || result.echo !== "가나") {
    throw new Error(`Backspace did not update DOM and C/WASM state: ${JSON.stringify(result)}`);
  }

  console.log("BROWSER_INPUT_OK backspace=DOM->C/WASM->DOM");
} finally {
  socket.close();
}
