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
      realtimeStatus: document.getElementById("realtime-status").textContent,
      chatDisabled: document.getElementById("chat-input").disabled,
      chatSendDisabled: document.getElementById("chat-send").disabled,
      chatStatus: document.getElementById("chat-status").textContent,
    };
  })()`);
  if (initial.value !== "가나다" || initial.echo !== "가나다"
      || initial.realtimeStatus !== "로그인 후 실시간 연결"
      || !initial.chatDisabled || !initial.chatSendDisabled
      || initial.chatStatus !== "로그인 후 채팅할 수 있습니다.") {
    throw new Error(`initial DOM/WASM input synchronization failed: ${JSON.stringify(initial)}`);
  }

  const safeChat = await evaluate(`(() => {
    appendChatMessage({
      sender_user_id: "<img src=x onerror=alert(1)>",
      text: "<script>globalThis.chatXss = true</" + "script>",
    });
    const messages = document.getElementById("chat-messages");
    return {
      text: messages.textContent,
      imageCount: messages.querySelectorAll("img").length,
      scriptCount: messages.querySelectorAll("script").length,
      executed: globalThis.chatXss === true,
    };
  })()`);
  if (safeChat.text !== "<img src=x onerror=alert(1)>: <script>globalThis.chatXss = true</script>"
      || safeChat.imageCount !== 0 || safeChat.scriptCount !== 0 || safeChat.executed) {
    throw new Error(`chat renderer did not preserve text safely: ${JSON.stringify(safeChat)}`);
  }

  const reconnectRuntime = await evaluate(`(() => {
    Module.ccall("BukClientProtocolRuntimeInit", null, [], []);
    const initial = {
      canSend: Module.ccall("BukClientCanSendStateCommands", "number", [], []),
      lastSequence: Module.ccall("BukClientLastSequence", "string", [], []),
    };
    const began = Module.ccall("BukClientBeginSynchronization", "number", [], []);
    const snapshot = Module.ccall(
      "BukClientApplySnapshotSequence", "number", ["string"], ["41"],
    );
    const event = Module.ccall(
      "BukClientApplyEventSequence", "number", ["string"], ["42"],
    );
    const completed = Module.ccall("BukClientCompleteSynchronization", "number", [], []);
    const confirmed = {
      canSend: Module.ccall("BukClientCanSendStateCommands", "number", [], []),
      lastSequence: Module.ccall("BukClientLastSequence", "string", [], []),
    };
    Module.ccall("BukClientBeginSynchronization", "number", [], []);
    Module.ccall("BukClientApplySnapshotSequence", "number", ["string"], ["50"]);
    const gapAccepted = Module.ccall(
      "BukClientApplyEventSequence", "number", ["string"], ["52"],
    );
    return {
      initial,
      began,
      snapshot,
      event,
      completed,
      confirmed,
      gapAccepted,
      requiresResync: Module.ccall("BukClientRequiresResynchronization", "number", [], []),
      preservedSequence: Module.ccall("BukClientLastSequence", "string", [], []),
      delays: Array.from({ length: 6 }, (_, attempt) => reconnectDelayForAttempt(attempt)),
    };
  })()`);
  if (reconnectRuntime.initial.canSend !== 0 || reconnectRuntime.initial.lastSequence !== "0"
      || reconnectRuntime.began !== 1 || reconnectRuntime.snapshot !== 1
      || reconnectRuntime.event !== 1 || reconnectRuntime.completed !== 1
      || reconnectRuntime.confirmed.canSend !== 1
      || reconnectRuntime.confirmed.lastSequence !== "42"
      || reconnectRuntime.gapAccepted !== 0 || reconnectRuntime.requiresResync !== 1
      || reconnectRuntime.preservedSequence !== "42"
      || JSON.stringify(reconnectRuntime.delays) !== "[250,500,1000,2000,5000,null]") {
    throw new Error(`reconnect runtime boundary failed: ${JSON.stringify(reconnectRuntime)}`);
  }

  const synchronizationBundle = await evaluate(`(() => {
    Module.ccall("BukClientProtocolRuntimeInit", null, [], []);
    const valid = applySynchronizationSequenceBundle({
      version: 1,
      direction: "server_response",
      type: "COMMAND_RESULT",
      command_id: "sync-1",
      room_id: "room-1",
      match_id: "match-1",
      payload: {
        status: "accepted",
        synchronization: {
          snapshot: { room_id: "room-1", match_id: "match-1", sequence: 41 },
          events: [{
            version: 1,
            direction: "server_event",
            type: "PLAYER_RECONNECTED",
            sequence: 42,
            room_id: "room-1",
            match_id: "match-1",
            payload: {},
          }],
        },
      },
    });
    const confirmed = Module.ccall("BukClientLastSequence", "string", [], []);
    const gap = applySynchronizationSequenceBundle({
      version: 1,
      direction: "server_response",
      type: "COMMAND_RESULT",
      command_id: "sync-2",
      room_id: "room-1",
      match_id: "match-1",
      payload: {
        status: "accepted",
        synchronization: {
          snapshot: { room_id: "room-1", match_id: "match-1", sequence: 50 },
          events: [{
            version: 1,
            direction: "server_event",
            type: "PLAYER_RECONNECTED",
            sequence: 52,
            room_id: "room-1",
            match_id: "match-1",
            payload: {},
          }],
        },
      },
    });
    return {
      valid,
      confirmed,
      gap,
      requiresResync: Module.ccall("BukClientRequiresResynchronization", "number", [], []),
      canSend: Module.ccall("BukClientCanSendStateCommands", "number", [], []),
      preserved: Module.ccall("BukClientLastSequence", "string", [], []),
    };
  })()`);
  if (!synchronizationBundle.valid || synchronizationBundle.confirmed !== "42"
      || synchronizationBundle.gap || synchronizationBundle.requiresResync !== 1
      || synchronizationBundle.canSend !== 0 || synchronizationBundle.preserved !== "42") {
    throw new Error(`synchronization bundle validation failed: ${JSON.stringify(synchronizationBundle)}`);
  }

  const prototypeRefreshSynchronization = await evaluate(`(() => {
    const originalWebSocket = globalThis.WebSocket;
    const instances = [];
    class CaptureWebSocket extends EventTarget {
      static CONNECTING = 0;
      static OPEN = 1;
      static CLOSING = 2;
      static CLOSED = 3;
      constructor(url) {
        super();
        this.url = url;
        this.readyState = CaptureWebSocket.CONNECTING;
        this.messages = [];
        instances.push(this);
      }
      send(text) { this.messages.push(JSON.parse(text)); }
      close() { this.readyState = CaptureWebSocket.CLOSED; }
      open() {
        this.readyState = CaptureWebSocket.OPEN;
        this.dispatchEvent(new Event("open"));
      }
    }
    globalThis.WebSocket = CaptureWebSocket;
    realtimeSocket = null;
    clearRealtimeReconnectTimer();
    clearStateReconnectScope();
    Module.ccall("BukClientProtocolRuntimeInit", null, [], []);
    wasmRuntimeReady = true;
    authenticatedUserId = null;
    showAuthenticated("usr_EREREREREREREREREREREQ");
    const capture = instances[0];
    capture.open();
    const reconnect = capture.messages[0];
    const blockedBeforeResponse = sendStateChangingCommand({ type: "SET_READY" });
    handleRealtimeMessage(capture, { data: JSON.stringify({
      version: 1,
      direction: "server_response",
      type: "COMMAND_RESULT",
      command_id: reconnect.command_id,
      room_id: "prototype-room",
      match_id: "prototype-match",
      payload: {
        status: "accepted",
        event_sequence_start: null,
        event_sequence_end: null,
        error: null,
        synchronization: {
          snapshot: { room_id: "prototype-room", match_id: "prototype-match", sequence: 1 },
          events: [],
        },
      },
    }) });
    const result = {
      instanceCount: instances.length,
      requestType: reconnect?.type,
      roomId: reconnect?.room_id,
      matchId: reconnect?.match_id,
      lastSequence: reconnect?.payload?.last_sequence,
      blockedBeforeResponse,
      pendingCount: pendingSynchronizationCommands.size,
      confirmedSequence: Module.ccall("BukClientLastSequence", "string", [], []),
      canSend: canSendStateChangingCommand(),
      status: realtimeStatus.textContent,
    };
    disconnectRealtime();
    authenticatedUserId = null;
    clearStateReconnectScope();
    globalThis.WebSocket = originalWebSocket;
    return result;
  })()`);
  if (prototypeRefreshSynchronization.instanceCount !== 1
      || prototypeRefreshSynchronization.requestType !== "RECONNECT"
      || prototypeRefreshSynchronization.roomId !== "prototype-room"
      || prototypeRefreshSynchronization.matchId !== "prototype-match"
      || prototypeRefreshSynchronization.lastSequence !== 0
      || prototypeRefreshSynchronization.blockedBeforeResponse
      || prototypeRefreshSynchronization.pendingCount !== 0
      || prototypeRefreshSynchronization.confirmedSequence !== "1"
      || !prototypeRefreshSynchronization.canSend
      || prototypeRefreshSynchronization.status !== "실시간 서버 상태 동기화 완료") {
    throw new Error(`prototype refresh synchronization failed: ${JSON.stringify(prototypeRefreshSynchronization)}`);
  }

  const automaticReconnect = await evaluate(`new Promise((resolve, reject) => {
    const originalWebSocket = globalThis.WebSocket;
    const instances = [];
    class FakeWebSocket extends EventTarget {
      static CONNECTING = 0;
      static OPEN = 1;
      static CLOSING = 2;
      static CLOSED = 3;
      constructor(url) {
        super();
        this.url = url;
        this.readyState = FakeWebSocket.CONNECTING;
        instances.push(this);
      }
      send() {}
      close() { this.readyState = FakeWebSocket.CLOSED; }
      open() {
        this.readyState = FakeWebSocket.OPEN;
        this.dispatchEvent(new Event("open"));
      }
      drop() {
        this.readyState = FakeWebSocket.CLOSED;
        this.dispatchEvent(new CloseEvent("close"));
      }
    }
    globalThis.WebSocket = FakeWebSocket;
    realtimeReconnectEnabled = true;
    realtimeReconnectAttempt = 4;
    clearRealtimeReconnectTimer();
    realtimeSocket = null;
    connectRealtime();
    instances[0].open();
    instances[0].drop();
    const scheduledStatus = realtimeStatus.textContent;
    setTimeout(() => {
      try {
        const result = {
          instanceCount: instances.length,
          scheduledStatus,
          reconnectingState: instances[1]?.readyState,
        };
        disconnectRealtime();
        globalThis.WebSocket = originalWebSocket;
        resolve(result);
      } catch (error) {
        globalThis.WebSocket = originalWebSocket;
        reject(error);
      }
    }, 350);
  })`, true);
  if (automaticReconnect.instanceCount !== 2
      || automaticReconnect.scheduledStatus !== "실시간 서버 재연결 대기 (250ms)"
      || automaticReconnect.reconnectingState !== 0) {
    throw new Error(`automatic reconnect failed: ${JSON.stringify(automaticReconnect)}`);
  }

  const logoutCancelsReconnect = await evaluate(`new Promise((resolve, reject) => {
    const originalWebSocket = globalThis.WebSocket;
    const instances = [];
    class FakeWebSocket extends EventTarget {
      static CONNECTING = 0;
      static OPEN = 1;
      static CLOSING = 2;
      static CLOSED = 3;
      constructor() {
        super();
        this.readyState = FakeWebSocket.CONNECTING;
        instances.push(this);
      }
      send() {}
      close() { this.readyState = FakeWebSocket.CLOSED; }
      open() {
        this.readyState = FakeWebSocket.OPEN;
        this.dispatchEvent(new Event("open"));
      }
      drop() {
        this.readyState = FakeWebSocket.CLOSED;
        this.dispatchEvent(new CloseEvent("close"));
      }
    }
    globalThis.WebSocket = FakeWebSocket;
    realtimeReconnectEnabled = true;
    realtimeReconnectAttempt = 0;
    clearRealtimeReconnectTimer();
    realtimeSocket = null;
    connectRealtime();
    instances[0].open();
    instances[0].drop();
    disconnectRealtime();
    setTimeout(() => {
      try {
        const result = {
          instanceCount: instances.length,
          reconnectEnabled: realtimeReconnectEnabled,
          timerCleared: realtimeReconnectTimer === null,
        };
        globalThis.WebSocket = originalWebSocket;
        resolve(result);
      } catch (error) {
        globalThis.WebSocket = originalWebSocket;
        reject(error);
      }
    }, 350);
  })`, true);
  if (logoutCancelsReconnect.instanceCount !== 1 || logoutCancelsReconnect.reconnectEnabled
      || !logoutCancelsReconnect.timerCleared) {
    throw new Error(`logout reconnect cancellation failed: ${JSON.stringify(logoutCancelsReconnect)}`);
  }

  const resyncRetry = await evaluate(`(() => {
    const originalWebSocket = globalThis.WebSocket;
    class CaptureWebSocket {
      static CONNECTING = 0;
      static OPEN = 1;
      static CLOSING = 2;
      static CLOSED = 3;
      constructor() {
        this.readyState = CaptureWebSocket.OPEN;
        this.messages = [];
      }
      send(text) { this.messages.push(JSON.parse(text)); }
      close() { this.readyState = CaptureWebSocket.CLOSED; }
    }
    globalThis.WebSocket = CaptureWebSocket;
    const capture = new CaptureWebSocket();
    realtimeSocket = capture;
    Module.ccall("BukClientProtocolRuntimeInit", null, [], []);
    Module.ccall("BukClientBeginSynchronization", "number", [], []);
    Module.ccall("BukClientApplySnapshotSequence", "number", ["string"], ["7"]);
    Module.ccall("BukClientCompleteSynchronization", "number", [], []);
    const scopeAccepted = setStateReconnectScope("room-1", "match-1");
    const first = capture.messages[0];
    const blockedBeforeSynchronization = sendStateChangingCommand({
      version: 1,
      direction: "client_command",
      type: "SET_READY",
      command_id: "blocked-command",
      room_id: "room-1",
      payload: { ready: true },
    });
    handleRealtimeMessage(capture, { data: JSON.stringify({
      version: 1,
      direction: "server_response",
      type: "COMMAND_RESULT",
      command_id: first.command_id,
      room_id: "room-1",
      match_id: "match-1",
      payload: {
        status: "rejected",
        event_sequence_start: null,
        event_sequence_end: null,
        error: { code: "RESYNC_REQUIRED", message: "retry", retriable: true },
        synchronization: null,
      },
    }) });
    const second = capture.messages[1];
    handleRealtimeMessage(capture, { data: JSON.stringify({
      version: 1,
      direction: "server_response",
      type: "COMMAND_RESULT",
      command_id: second.command_id,
      room_id: "room-1",
      match_id: "match-1",
      payload: {
        status: "accepted",
        event_sequence_start: null,
        event_sequence_end: null,
        error: null,
        synchronization: {
          snapshot: { room_id: "room-1", match_id: "match-1", sequence: 8 },
          events: [{
            version: 1,
            direction: "server_event",
            type: "PLAYER_RECONNECTED",
            sequence: 9,
            room_id: "room-1",
            match_id: "match-1",
            payload: {},
          }],
        },
      },
    }) });
    const syncRequestCount = capture.messages.length;
    const sentAfterSynchronization = sendStateChangingCommand({
      version: 1,
      direction: "client_command",
      type: "SET_READY",
      command_id: "allowed-command",
      room_id: "room-1",
      payload: { ready: true },
    });
    const result = {
      scopeAccepted,
      blockedBeforeSynchronization,
      syncRequestCount,
      firstLastSequence: first.payload.last_sequence,
      secondLastSequence: second.payload.last_sequence,
      commandIDsDiffer: first.command_id !== second.command_id,
      canSend: canSendStateChangingCommand(),
      sentAfterSynchronization,
      sentCommandType: capture.messages[2]?.type,
      lastSequence: Module.ccall("BukClientLastSequence", "string", [], []),
      status: realtimeStatus.textContent,
    };
    clearStateReconnectScope();
    realtimeSocket = null;
    globalThis.WebSocket = originalWebSocket;
    return result;
  })()`);
  if (!resyncRetry.scopeAccepted || resyncRetry.blockedBeforeSynchronization
      || resyncRetry.syncRequestCount !== 2
      || resyncRetry.firstLastSequence !== 7 || resyncRetry.secondLastSequence !== 0
      || !resyncRetry.commandIDsDiffer || !resyncRetry.canSend
      || !resyncRetry.sentAfterSynchronization || resyncRetry.sentCommandType !== "SET_READY"
      || resyncRetry.lastSequence !== "9"
      || resyncRetry.status !== "실시간 서버 상태 동기화 완료") {
    throw new Error(`RESYNC_REQUIRED retry failed: ${JSON.stringify(resyncRetry)}`);
  }

  const staleScopeResponse = await evaluate(`(() => {
    const originalWebSocket = globalThis.WebSocket;
    class CaptureWebSocket {
      static OPEN = 1;
      constructor() { this.readyState = CaptureWebSocket.OPEN; this.messages = []; }
      send(text) { this.messages.push(JSON.parse(text)); }
      close() {}
    }
    globalThis.WebSocket = CaptureWebSocket;
    const capture = new CaptureWebSocket();
    realtimeSocket = capture;
    Module.ccall("BukClientProtocolRuntimeInit", null, [], []);
    setStateReconnectScope("room-old", "match-old");
    const staleCommand = capture.messages[0];
    setStateReconnectScope("room-new", "match-new");
    handleRealtimeMessage(capture, { data: JSON.stringify({
      version: 1,
      direction: "server_response",
      type: "COMMAND_RESULT",
      command_id: staleCommand.command_id,
      room_id: "room-old",
      match_id: "match-old",
      payload: {
        status: "accepted",
        event_sequence_start: null,
        event_sequence_end: null,
        error: null,
        synchronization: {
          snapshot: { room_id: "room-old", match_id: "match-old", sequence: 1 },
          events: [],
        },
      },
    }) });
    const result = {
      requestCount: capture.messages.length,
      pendingCount: pendingSynchronizationCommands.size,
      lastSequence: Module.ccall("BukClientLastSequence", "string", [], []),
      canSend: canSendStateChangingCommand(),
    };
    clearStateReconnectScope();
    realtimeSocket = null;
    globalThis.WebSocket = originalWebSocket;
    return result;
  })()`);
  if (staleScopeResponse.requestCount !== 2 || staleScopeResponse.pendingCount !== 1
      || staleScopeResponse.lastSequence !== "0" || staleScopeResponse.canSend) {
    throw new Error(`stale reconnect scope response was applied: ${JSON.stringify(staleScopeResponse)}`);
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

  console.log("BROWSER_INPUT_OK backspace=DOM->C/WASM->DOM reconnect=prototype-scope->JS->C/WASM");
} finally {
  socket.close();
}
