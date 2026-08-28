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

  await evaluate(`(() => {
    globalThis.makeTestGameSnapshot = (roomId, matchId, sequence) => ({
      room_id: roomId,
      match_id: matchId,
      sequence,
      status: "active",
      teams: [
        { team_id: "A", player_ids: ["user-a"], turn_order: ["user-a"] },
        { team_id: "B", player_ids: ["user-b"], turn_order: ["user-b"] },
      ],
      participants: [
        {
          user_id: "user-a",
          nickname: "가람",
          role: "player",
          team_id: "A",
          permissions: ["control_game", "chat"],
          connected: true,
          cpu_control: { active: false, reason: null },
        },
        {
          user_id: "user-b",
          nickname: "나래",
          role: "player",
          team_id: "B",
          permissions: ["control_game", "chat"],
          connected: true,
          cpu_control: { active: false, reason: null },
        },
      ],
      current_turn: {
        player_id: "user-a",
        phase: "wait_piece_selection",
        required_input: "select_piece",
        move_request: {
          required_input: "select_piece",
          token_ids: ["token-1"],
          piece_ids: ["A-1"],
          routes: [],
        },
        timer: { phase: "move", remaining_ms: 52000, deadline_at: null },
      },
      result_queue: [{
        token_id: "token-1",
        result: "gae",
        origin: "initial_throw",
        generated_by_player_id: "user-a",
      }],
      pieces: [
        {
          piece_id: "A-1",
          team_id: "A",
          state: "on_board",
          current_space_id: "do",
          stack_id: null,
          position_group_id: "group-A-do",
          actual_previous_space: "chammeogi",
        },
        {
          piece_id: "B-1",
          team_id: "B",
          state: "waiting",
          current_space_id: null,
          stack_id: null,
          position_group_id: null,
          actual_previous_space: null,
        },
      ],
      stacks: [],
      position_groups: [{
        group_id: "group-A-do",
        team_id: "A",
        space_id: "do",
        piece_ids: ["A-1"],
      }],
      buk: { enabled: false, destination_space_id: null },
      pause: { used: false, paused: false, ends_at: null },
    });
    globalThis.makeTestRouteSnapshot = (roomId, matchId, sequence) => {
      const snapshot = makeTestGameSnapshot(roomId, matchId, sequence);
      snapshot.current_turn.phase = "wait_route_selection";
      snapshot.current_turn.required_input = "select_route";
      snapshot.current_turn.move_request = {
        required_input: "select_route",
        token_ids: ["token-1"],
        piece_ids: ["A-1"],
        routes: ["normal", "shortcut"],
      };
      snapshot.pieces[0].current_space_id = "mo";
      snapshot.pieces[0].position_group_id = "group-A-mo";
      snapshot.pieces[0].actual_previous_space = "yut";
      snapshot.position_groups[0] = {
        group_id: "group-A-mo",
        team_id: "A",
        space_id: "mo",
        piece_ids: ["A-1"],
      };
      return snapshot;
    };
    return true;
  })()`);

  const initial = await evaluate(`(() => {
    const input = document.getElementById("ime-input");
    const canvas = document.getElementById("canvas");
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
      roomListStatus: document.getElementById("room-list-status").textContent,
      roomRefreshDisabled: document.getElementById("room-refresh").disabled,
      canvasWidth: canvas.width,
      canvasHeight: canvas.height,
      canvasAspectRatio: canvas.clientWidth / canvas.clientHeight,
      renderedBoardNodeCount: Module.ccall(
        "BukClientRenderedBoardNodeCount", "number", [], [],
      ),
      renderedBoardEdgeCount: Module.ccall(
        "BukClientRenderedBoardEdgeCount", "number", [], [],
      ),
      renderedPieceCount: Module.ccall(
        "BukClientRenderedPieceCount", "number", [], [],
      ),
      assetsInitialized: Module.ccall("BukClientAssetsInitialized", "number", [], []),
      assetsLoadedCount: Module.ccall("BukClientAssetsLoadedCount", "number", [], []),
      assetsFallbackCount: Module.ccall("BukClientAssetsFallbackCount", "number", [], []),
    };
  })()`);
  if (initial.value !== "가나다" || initial.echo !== "가나다"
      || initial.realtimeStatus !== "로그인 후 실시간 연결"
      || !initial.chatDisabled || !initial.chatSendDisabled
      || initial.chatStatus !== "로그인 후 채팅할 수 있습니다."
      || initial.roomListStatus !== "로그인 후 방 목록을 확인할 수 있습니다."
      || !initial.roomRefreshDisabled
      || initial.canvasWidth !== 1280 || initial.canvasHeight !== 720
      || Math.abs(initial.canvasAspectRatio - (16 / 9)) > 0.01
      || initial.renderedBoardNodeCount !== 29
      || initial.renderedBoardEdgeCount !== 32 || initial.renderedPieceCount !== 0
      || initial.assetsInitialized !== 1 || initial.assetsLoadedCount !== 46
      || initial.assetsFallbackCount !== 0) {
    throw new Error(`initial DOM/WASM input synchronization failed: ${JSON.stringify(initial)}`);
  }

  await command("Emulation.setDeviceMetricsOverride", {
    width: 390, height: 844, deviceScaleFactor: 1, mobile: true,
  });
  const mobileLayout = await evaluate(`(() => {
    roomListAuthenticated = true;
    renderRoomList([{
      room_id: "room-with-an-intentionally-very-long-identifier-for-mobile",
      title: "아주 긴 방 제목이 좁은 모바일 화면에서도 안전하게 줄바꿈되어야 합니다",
      has_password: false, player_count: 4, max_players: 8,
    }]);
    activeRoomId = "room-with-an-intentionally-very-long-identifier-for-mobile";
    authenticatedUserId = "mobile-user";
    renderRoomDetail({
      summary: { room_id: activeRoomId, title: "모바일 상세 화면의 긴 방 제목", has_password: false,
        player_count: 4, max_players: 8 },
      members: [{ user_id: "mobile-user-with-a-long-id", role: "player", team: "A", ready: false }],
    });
    const roomCreate = document.getElementById("room-create-form");
    const chatForm = document.getElementById("chat-form");
    const canvas = document.getElementById("canvas");
    return {
      viewportWidth: window.innerWidth,
      documentWidth: document.documentElement.scrollWidth,
      roomCreateColumns: getComputedStyle(roomCreate).gridTemplateColumns,
      chatColumns: getComputedStyle(chatForm).gridTemplateColumns,
      canvasAspectRatio: canvas.clientWidth / canvas.clientHeight,
      touchTarget: getComputedStyle(document.getElementById("room-team-a")).minHeight,
      roomTitleOverflow: getComputedStyle(document.querySelector("#room-list strong")).overflowY,
      detailTitleMaxHeight: getComputedStyle(document.getElementById("room-detail-title")).maxHeight,
    };
  })()`);
  if (mobileLayout.viewportWidth !== 390 || mobileLayout.documentWidth > mobileLayout.viewportWidth
      || mobileLayout.roomCreateColumns === "none" || mobileLayout.chatColumns === "none"
      || Math.abs(mobileLayout.canvasAspectRatio - (16 / 9)) > 0.01
      || mobileLayout.touchTarget !== "44px"
      || mobileLayout.roomTitleOverflow !== "auto"
      || mobileLayout.detailTitleMaxHeight === "none") {
    throw new Error(`mobile shell layout overflowed or did not reflow: ${JSON.stringify(mobileLayout)}`);
  }
  await evaluate(`(() => {
    activeRoomId = null;
    authenticatedUserId = null;
    roomListAuthenticated = false;
    roomDetail.hidden = true;
    roomMembers.replaceChildren();
    setRoomLobbyControls(false);
  })()`);
  await command("Emulation.clearDeviceMetricsOverride");

  const safeRoomList = await evaluate(`(() => {
    roomListAuthenticated = true;
    renderRoomList([{
      room_id: "r-1", title: "<script>bad</script>", has_password: true,
      player_count: 2, max_players: 4,
    }]);
    const list = document.getElementById("room-list");
    return {
      text: list.textContent,
      scriptCount: list.querySelectorAll("script").length,
      status: document.getElementById("room-list-status").textContent,
    };
  })()`);
  if (safeRoomList.text !== "<script>bad</script>2/4명 · 비밀번호방 ID: r-1플레이어 참여관전"
      || safeRoomList.scriptCount !== 0 || safeRoomList.status !== "1개의 공개 방") {
    throw new Error(`room list renderer was not safe/deterministic: ${JSON.stringify(safeRoomList)}`);
  }
  const roomDetail = await evaluate(`(() => {
    renderRoomDetail({
      summary: { room_id: "r-1", title: "방", has_password: false, player_count: 1, max_players: 2 },
      members: [{ user_id: "user-1", role: "player", team: "A", ready: true }],
      active_start: undefined,
    });
    return {
      hidden: document.getElementById("room-detail").hidden,
      title: document.getElementById("room-detail-title").textContent,
      status: document.getElementById("room-detail-status").textContent,
      member: document.getElementById("room-members").textContent,
      controlsDisabled: ["room-team-a", "room-team-b", "room-ready", "room-start"]
        .every((id) => document.getElementById(id).disabled),
    };
  })()`);
  if (roomDetail.hidden || roomDetail.title !== "방"
      || roomDetail.status !== "1/2명 · 대기 중"
      || roomDetail.member !== "user-1 · player · A팀 · 준비 완료"
      || !roomDetail.controlsDisabled) {
    throw new Error(`room detail renderer failed: ${JSON.stringify(roomDetail)}`);
  }
  const invalidRoomSummary = await evaluate(`(() => ({
    invalidCount: validateRoomSummary({
      room_id: "r-2", title: "bad", has_password: false,
      player_count: 5, max_players: 4,
    }),
    invalidShape: validateRoomSummary({ room_id: "r-3" }),
  }))()`);
  if (invalidRoomSummary.invalidCount || invalidRoomSummary.invalidShape) {
    throw new Error(`room summary validator accepted invalid payload: ${JSON.stringify(invalidRoomSummary)}`);
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
    const metadata = Module.ccall(
      "BukClientStageSnapshotMetadata", "number",
      ["string", "string", "string", "string", "string", "string"],
      ["active", "wait_throw", "throw", "throw", "A", "20000"],
    );
    const piece = Module.ccall(
      "BukClientStageSnapshotPiece", "number", ["string", "string", "string", "number", "number"],
      ["A", "on_board", "do", 0, 0],
    );
    const resultToken = Module.ccall(
      "BukClientStageSnapshotResult", "number", ["string"], ["gae"],
    );
    const completed = Module.ccall("BukClientCompleteSynchronization", "number", [], []);
    const confirmed = {
      canSend: Module.ccall("BukClientCanSendStateCommands", "number", [], []),
      lastSequence: Module.ccall("BukClientLastSequence", "string", [], []),
    };
    Module.ccall("BukClientBeginSynchronization", "number", [], []);
    Module.ccall("BukClientApplySnapshotSequence", "number", ["string"], ["50"]);
    Module.ccall(
      "BukClientStageSnapshotMetadata", "number",
      ["string", "string", "string", "string", "string", "string"],
      ["active", "wait_throw", "throw", "throw", "B", "19000"],
    );
    const gapAccepted = Module.ccall(
      "BukClientApplyEventSequence", "number", ["string"], ["52"],
    );
    return {
      initial,
      began,
      snapshot,
      metadata,
      piece,
      resultToken,
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
      || reconnectRuntime.metadata !== 1 || reconnectRuntime.piece !== 1
      || reconnectRuntime.resultToken !== 1 || reconnectRuntime.completed !== 1
      || reconnectRuntime.confirmed.canSend !== 1
      || reconnectRuntime.confirmed.lastSequence !== "41"
      || reconnectRuntime.gapAccepted !== 0 || reconnectRuntime.requiresResync !== 1
      || reconnectRuntime.preservedSequence !== "41"
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
          snapshot: makeTestGameSnapshot("room-1", "match-1", 41),
          events: [],
        },
      },
    });
    const confirmed = Module.ccall("BukClientLastSequence", "string", [], []);
    const presentation = {
      status: Module.ccall("BukClientPresentationStatus", "string", [], []),
      phase: Module.ccall("BukClientPresentationTurnPhase", "string", [], []),
      requiredInput: Module.ccall("BukClientPresentationRequiredInput", "string", [], []),
      currentTeam: Module.ccall("BukClientPresentationCurrentTeam", "string", [], []),
      remainingMs: Module.ccall(
        "BukClientPresentationRemainingMilliseconds", "string", [], [],
      ),
      pieceCount: Module.ccall("BukClientPresentationPieceCount", "number", [], []),
      resultCount: Module.ccall("BukClientPresentationResultCount", "number", [], []),
      pieceIds: [...confirmedSnapshotPieceIds],
    };
    const invalidSnapshot = makeTestGameSnapshot("room-1", "match-1", 42);
    invalidSnapshot.pieces[0].current_space_id = "not-a-canonical-space";
    const invalidSpace = applySynchronizationSequenceBundle({
      version: 1,
      direction: "server_response",
      type: "COMMAND_RESULT",
      command_id: "sync-invalid-space",
      room_id: "room-1",
      match_id: "match-1",
      payload: {
        status: "accepted",
        synchronization: { snapshot: invalidSnapshot, events: [] },
      },
    });
    const preservedAfterInvalid = {
      sequence: Module.ccall("BukClientLastSequence", "string", [], []),
      status: Module.ccall("BukClientPresentationStatus", "string", [], []),
      pieceIds: [...confirmedSnapshotPieceIds],
    };
    const invalidRouteSnapshot = makeTestRouteSnapshot("room-1", "match-1", 43);
    invalidRouteSnapshot.current_turn.move_request.routes = ["shortcut"];
    const invalidRouteRequest = applySynchronizationSequenceBundle({
      version: 1,
      direction: "server_response",
      type: "COMMAND_RESULT",
      command_id: "sync-invalid-route",
      room_id: "room-1",
      match_id: "match-1",
      payload: {
        status: "accepted",
        synchronization: { snapshot: invalidRouteSnapshot, events: [] },
      },
    });
    const invalidStackSnapshot = makeTestGameSnapshot("room-1", "match-1", 44);
    invalidStackSnapshot.stacks = [{
      stack_id: "stack-A-do",
      team_id: "A",
      space_id: "do",
      piece_ids: ["missing-piece", "A-1"],
      actual_previous_space: "chammeogi",
    }];
    invalidStackSnapshot.pieces[0].stack_id = "stack-A-do";
    const invalidStack = applySynchronizationSequenceBundle({
      type: "COMMAND_RESULT",
      direction: "server_response",
      version: 1,
      room_id: "room-1",
      match_id: "match-1",
      payload: {
        status: "accepted",
        synchronization: { snapshot: invalidStackSnapshot, events: [] },
      },
    });
    const eventTail = applySynchronizationSequenceBundle({
      version: 1,
      direction: "server_response",
      type: "COMMAND_RESULT",
      command_id: "sync-2",
      room_id: "room-1",
      match_id: "match-1",
      payload: {
        status: "accepted",
        synchronization: {
          snapshot: makeTestGameSnapshot("room-1", "match-1", 50),
          events: [{
            version: 1,
            direction: "server_event",
            type: "PLAYER_RECONNECTED",
            sequence: 51,
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
      presentation,
      invalidSpace,
      invalidRouteRequest,
      invalidStack,
      preservedAfterInvalid,
      eventTail,
      requiresResync: Module.ccall("BukClientRequiresResynchronization", "number", [], []),
      canSend: Module.ccall("BukClientCanSendStateCommands", "number", [], []),
      preserved: Module.ccall("BukClientLastSequence", "string", [], []),
    };
  })()`);
  if (!synchronizationBundle.valid || synchronizationBundle.confirmed !== "41"
      || synchronizationBundle.presentation.status !== "active"
      || synchronizationBundle.presentation.phase !== "wait_piece_selection"
      || synchronizationBundle.presentation.requiredInput !== "select_piece"
      || synchronizationBundle.presentation.currentTeam !== "A"
      || synchronizationBundle.presentation.remainingMs !== "52000"
      || synchronizationBundle.presentation.pieceCount !== 2
      || synchronizationBundle.presentation.resultCount !== 1
      || JSON.stringify(synchronizationBundle.presentation.pieceIds) !== '["A-1","B-1"]'
      || synchronizationBundle.invalidSpace
      || synchronizationBundle.invalidRouteRequest
      || synchronizationBundle.invalidStack
      || synchronizationBundle.preservedAfterInvalid.sequence !== "41"
      || synchronizationBundle.preservedAfterInvalid.status !== "active"
      || JSON.stringify(synchronizationBundle.preservedAfterInvalid.pieceIds)
         !== '["A-1","B-1"]'
      || synchronizationBundle.eventTail || synchronizationBundle.requiresResync !== 1
      || synchronizationBundle.canSend !== 0 || synchronizationBundle.preserved !== "41") {
    throw new Error(`synchronization bundle validation failed: ${JSON.stringify(synchronizationBundle)}`);
  }

  const renderedSnapshot = await evaluate(`new Promise((resolve) => {
    requestAnimationFrame(() => requestAnimationFrame(() => resolve({
      pieces: Module.ccall("BukClientRenderedPieceCount", "number", [], []),
      hasSnapshot: Module.ccall("BukClientHasPresentationSnapshot", "number", [], []),
    })));
  })`, true);
  if (renderedSnapshot.pieces !== 1 || renderedSnapshot.hasSnapshot !== 1) {
    throw new Error(`authoritative pieces were not rendered: ${JSON.stringify(renderedSnapshot)}`);
  }

  const routeSelection = await evaluate(`new Promise((resolve) => {
    class RouteWebSocket {
      static OPEN = 1;
      constructor() { this.readyState = RouteWebSocket.OPEN; this.messages = []; }
      send(text) { this.messages.push(JSON.parse(text)); }
      close() {}
    }
    const originalWebSocket = globalThis.WebSocket;
    const capture = new RouteWebSocket();
    globalThis.WebSocket = RouteWebSocket;
    realtimeSocket = capture;
    authenticatedUserId = "user-a";
    stateReconnectScope = { roomId: "room-route", matchId: "match-route" };
    pendingSynchronizationCommands.clear();
    pendingRouteCommands.clear();
    Module.ccall("BukClientProtocolRuntimeInit", null, [], []);
    const applied = applySynchronizationSequenceBundle({
      version: 1,
      direction: "server_response",
      type: "COMMAND_RESULT",
      room_id: "room-route",
      match_id: "match-route",
      payload: {
        status: "accepted",
        synchronization: {
          snapshot: makeTestRouteSnapshot("room-route", "match-route", 60),
          events: [],
        },
      },
    });
    const opponentLocked = (() => {
      authenticatedUserId = "user-b";
      refreshRouteInteraction();
      const locked = Module.ccall("BukClientCanSelectRoute", "number", [], []) === 0;
      authenticatedUserId = "user-a";
      refreshRouteInteraction();
      return locked;
    })();
    const requested = Module.ccall(
      "BukClientRequestRouteSelection", "number", ["string"], ["shortcut"],
    );
    const drained = drainRouteSelectionIntent();
    const first = capture.messages[0];
    const duplicateBlocked = Module.ccall(
      "BukClientRequestRouteSelection", "number", ["string"], ["normal"],
    ) === 0;
    handleRealtimeMessage(capture, { data: JSON.stringify({
      version: 1,
      direction: "server_response",
      type: "COMMAND_RESULT",
      command_id: first.command_id,
      room_id: "room-route",
      match_id: "match-route",
      payload: {
        status: "rejected",
        error: { code: "INVALID_TURN_ACTION", message: "retry", retriable: false },
      },
    }) });
    const retryEnabled = Module.ccall("BukClientCanSelectRoute", "number", [], []) === 1;
    const retryRequested = Module.ccall(
      "BukClientRequestRouteSelection", "number", ["string"], ["normal"],
    );
    const retryDrained = drainRouteSelectionIntent();
    const second = capture.messages[1];
    handleRealtimeMessage(capture, { data: JSON.stringify({
      version: 1,
      direction: "server_response",
      type: "COMMAND_RESULT",
      command_id: second.command_id,
      room_id: "room-route",
      match_id: "match-route",
      payload: { status: "accepted", error: null },
    }) });
    const reconnect = capture.messages[2];
    requestAnimationFrame(() => requestAnimationFrame(() => {
      const result = {
        applied,
        opponentLocked,
        requested,
        drained,
        duplicateBlocked,
        retryEnabled,
        retryRequested,
        retryDrained,
        first,
        second,
        reconnect,
        routeOptions: Module.ccall("BukClientRenderedRouteOptionCount", "number", [], []),
        highlightedEdges: Module.ccall("BukClientHighlightedRouteEdgeCount", "number", [], []),
        lockedDuringSync: Module.ccall("BukClientCanSelectRoute", "number", [], []) === 0,
      };
      pendingSynchronizationCommands.clear();
      pendingRouteCommands.clear();
      stateReconnectScope = null;
      confirmedRouteRequest = null;
      realtimeSocket = null;
      authenticatedUserId = null;
      globalThis.WebSocket = originalWebSocket;
      resolve(result);
    }));
  })`, true);
  if (!routeSelection.applied || !routeSelection.opponentLocked
      || routeSelection.requested !== 1 || !routeSelection.drained
      || !routeSelection.duplicateBlocked || !routeSelection.retryEnabled
      || routeSelection.retryRequested !== 1 || !routeSelection.retryDrained
      || routeSelection.first?.type !== "SELECT_ROUTE"
      || routeSelection.first?.room_id !== "room-route"
      || routeSelection.first?.match_id !== "match-route"
      || routeSelection.first?.payload?.token_id !== "token-1"
      || routeSelection.first?.payload?.piece_id !== "A-1"
      || routeSelection.first?.payload?.route !== "shortcut"
      || routeSelection.second?.payload?.route !== "normal"
      || routeSelection.reconnect?.type !== "RECONNECT"
      || routeSelection.reconnect?.payload?.last_sequence !== 60
      || routeSelection.routeOptions !== 2 || routeSelection.highlightedEdges !== 2
      || !routeSelection.lockedDuringSync) {
    throw new Error(`authoritative route selection failed: ${JSON.stringify(routeSelection)}`);
  }

  // ADR-0013's fabricated prototype scope is retired: login must not send a
  // RECONNECT on its own. The synchronization machinery is then exercised by
  // setting an explicit scope, as the future lobby screens will do with a
  // live GAME_STARTING match_id.
  const explicitScopeSynchronization = await evaluate(`(() => {
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
    const commandsAfterLogin = capture.messages.length;
    const scopeSet = setStateReconnectScope("room-82", "match-82");
    const reconnect = capture.messages[commandsAfterLogin];
    const blockedBeforeResponse = sendStateChangingCommand({ type: "SET_READY" });
    handleRealtimeMessage(capture, { data: JSON.stringify({
      version: 1,
      direction: "server_response",
      type: "COMMAND_RESULT",
      command_id: reconnect.command_id,
      room_id: "room-82",
      match_id: "match-82",
      payload: {
        status: "accepted",
        event_sequence_start: null,
        event_sequence_end: null,
        error: null,
        synchronization: {
          snapshot: makeTestGameSnapshot("room-82", "match-82", 1),
          events: [],
        },
      },
    }) });
    const result = {
      instanceCount: instances.length,
      commandsAfterLogin,
      scopeSet,
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
  if (explicitScopeSynchronization.instanceCount !== 1
      || explicitScopeSynchronization.commandsAfterLogin !== 0
      || !explicitScopeSynchronization.scopeSet
      || explicitScopeSynchronization.requestType !== "RECONNECT"
      || explicitScopeSynchronization.roomId !== "room-82"
      || explicitScopeSynchronization.matchId !== "match-82"
      || explicitScopeSynchronization.lastSequence !== 0
      || explicitScopeSynchronization.blockedBeforeResponse
      || explicitScopeSynchronization.pendingCount !== 0
      || explicitScopeSynchronization.confirmedSequence !== "1"
      || !explicitScopeSynchronization.canSend
      || explicitScopeSynchronization.status !== "실시간 서버 상태 동기화 완료") {
    throw new Error(`explicit scope synchronization failed: ${JSON.stringify(explicitScopeSynchronization)}`);
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
    applySynchronizationSequenceBundle({
      version: 1,
      direction: "server_response",
      type: "COMMAND_RESULT",
      room_id: "room-1",
      match_id: "match-1",
      payload: {
        status: "accepted",
        synchronization: {
          snapshot: makeTestGameSnapshot("room-1", "match-1", 7),
          events: [],
        },
      },
    });
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
          snapshot: makeTestGameSnapshot("room-1", "match-1", 9),
          events: [],
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
          snapshot: makeTestGameSnapshot("room-old", "match-old", 1),
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

  const eventCue = await evaluate(`(() => {
    stateReconnectScope = { roomId: "room-cue", matchId: "match-cue" };
    lastGameEventSequence = 10;
    const backdo = acceptGameEventCue({
      version: 1, direction: "server_event", room_id: "room-cue", match_id: "match-cue",
      sequence: 11, type: "PIECE_MOVED",
      payload: { movement_kind: "backdo" },
    });
    const wrongScope = acceptGameEventCue({
      version: 1, direction: "server_event", room_id: "other", match_id: "match-cue",
      sequence: 12, type: "PIECE_MOVED", payload: { movement_kind: "buk" },
    });
    const duplicate = acceptGameEventCue({
      version: 1, direction: "server_event", room_id: "room-cue", match_id: "match-cue",
      sequence: 11, type: "PIECE_MOVED", payload: { movement_kind: "buk" },
    });
    const malformed = acceptGameEventCue({
      version: 1, direction: "server_event", room_id: "room-cue", match_id: "match-cue",
      sequence: 12, type: "BUK_RESOLVED", payload: { no_candidate: false },
    });
    const buk = acceptGameEventCue({
      version: 1, direction: "server_event", room_id: "room-cue", match_id: "match-cue",
      sequence: 12, type: "BUK_RESOLVED", payload: {
        token_id: "token-1", destination_space_id: "do", no_candidate: false,
      },
    });
    const result = { backdo, wrongScope, duplicate, malformed, buk,
      cue: eventCue?.kind, sequence: eventCue?.sequence };
    Module.ccall("BukClientClearEventCue", "number", [], []);
    clearEventCue();
    clearStateReconnectScope();
    return result;
  })()`);
  if (!eventCue.backdo || eventCue.wrongScope || eventCue.duplicate
      || eventCue.malformed || !eventCue.buk || eventCue.cue !== "buk"
      || eventCue.sequence !== 12) {
    throw new Error(`authoritative backdo/buk cue validation failed: ${JSON.stringify(eventCue)}`);
  }

  const replayTail = await evaluate(`(() => {
    const snapshot = makeTestGameSnapshot("room-replay", "match-replay", 50);
    const applied = applySynchronizationSequenceBundle({
      version: 1, direction: "server_response", type: "COMMAND_RESULT",
      room_id: "room-replay", match_id: "match-replay",
      payload: { status: "accepted", synchronization: { snapshot, events: [{
        version: 1, direction: "server_event", type: "TURN_STARTED", sequence: 51,
        room_id: "room-replay", match_id: "match-replay",
        payload: { player_id: "user-a", phase: "wait_throw", required_input: "throw", remaining_ms: 19000 },
      }] } },
    });
    return {
      applied,
      lastSequence: Module.ccall("BukClientLastSequence", "string", [], []),
      phase: Module.ccall("BukClientPresentationTurnPhase", "string", [], []),
    };
  })()`);
  if (!replayTail.applied || replayTail.lastSequence !== "51"
      || replayTail.phase !== "wait_throw") {
    throw new Error(`authoritative replay tail failed: ${JSON.stringify(replayTail)}`);
  }

  const replayTopology = await evaluate(`(() => {
    const snapshot = makeTestGameSnapshot("room-topology", "match-topology", 60);
    snapshot.pieces.push({
      piece_id: "A-2", team_id: "A", state: "on_board", current_space_id: "do",
      stack_id: "stack-A-do", position_group_id: "group-A-do", actual_previous_space: null,
    });
    snapshot.pieces[0].stack_id = "stack-A-do";
    snapshot.stacks = [{ stack_id: "stack-A-do", team_id: "A", space_id: "do",
      piece_ids: ["A-1", "A-2"], actual_previous_space: null }];
    snapshot.position_groups[0].piece_ids = ["A-1", "A-2"];
    const moved = reduceReplayEvents(snapshot, [
      { version: 1, direction: "server_event", type: "PIECE_MOVED", sequence: 61,
        room_id: "room-topology", match_id: "match-topology",
        payload: { piece_ids: ["A-1", "A-2"], from_space_id: "do", to_space_id: "gae",
          movement_kind: "forward" } },
      { version: 1, direction: "server_event", type: "PIECES_STACKED", sequence: 62,
        room_id: "room-topology", match_id: "match-topology",
        payload: { stack_id: "stack:A:gae", team_id: "A", space_id: "gae",
          piece_ids: ["A-1", "A-2"], actual_previous_space: "do" } },
    ]);
    const malformed = reduceReplayEvents(snapshot, [{
      version: 2, direction: "server_event", type: "TURN_STARTED", sequence: 61,
      room_id: "room-topology", match_id: "match-topology",
      payload: { player_id: "user-a", phase: "wait_throw", required_input: "throw", remaining_ms: 1 },
    }]);
    return { movedStacks: moved?.stacks, movedSpace: moved?.stacks?.[0]?.space_id,
      movedStackId: moved?.stacks?.[0]?.stack_id, malformed };
  })()`);
  if (replayTopology.movedSpace !== "gae" || replayTopology.movedStackId !== "stack:A:gae"
      || replayTopology.movedStacks?.length !== 1 || replayTopology.movedStacks?.[0]?.piece_ids?.length !== 2
      || replayTopology.malformed !== null) {
    throw new Error(`replay topology reducer failed: ${JSON.stringify(replayTopology)}`);
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

  console.log("BROWSER_INPUT_OK backspace=DOM->C/WASM->DOM reconnect=prototype-scope->JS->C/WASM route=authoritative-snapshot->SELECT_ROUTE cue=PIECE_MOVED/BUK_RESOLVED->HUD");
} finally {
  socket.close();
}
