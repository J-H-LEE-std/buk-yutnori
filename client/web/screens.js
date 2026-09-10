/* Screen ownership and accessible game presentation; no rule calculations. */
globalThis.BukScreens = (() => {
  const main = document.querySelector('main');
  const auth = document.querySelector('.auth');
  const lobby = document.querySelector('.lobby');
  const stage = document.querySelector('.game-stage');
  const chat = document.querySelector('.chat');
  const diagnostics = document.querySelector('.bridge');
  const make = (tag, id, text = '') => {
    const node = document.createElement(tag);
    if (id) node.id = id;
    node.textContent = text;
    return node;
  };
  const brand = make('div', null, '북 · 윷놀이'); brand.className = 'brand'; auth.prepend(brand);
  const shell = make('div'); shell.className = 'screen-shell';
  const content = make('div');
  main.prepend(shell); shell.append(auth, content);
  const views = {};
  for (const name of ['lobby', 'room', 'game']) {
    views[name] = make('section', `screen-${name}`);
    views[name].className = 'screen'; views[name].hidden = true;
    views[name].setAttribute('aria-label', {lobby:'메인 로비', room:'방 로비', game:'게임'}[name]);
    content.append(views[name]);
  }
  views.lobby.append(lobby);
  const settingsPanel = make('details','create-settings');
  settingsPanel.append(make('summary',null,'게임 상세 설정'));
  const settingsFields = new Map();
  const settingDefinitions = [
    ['max_players','최대 인원',8,[2,4,6,8]], ['piece_count','팀별 말 수',4,[2,3,4]],
    ['stacking_enabled','업기',true,[true,false]],
    ['capture_extra_throw','잡기 추가 던지기','always',['always','do_to_geol_plus_special','none']],
    ['yut_mo_extra_throw','윷·모 추가 던지기',true,[true,false]],
    ['shortcut_policy','지름길','selectable',['selectable','forced']],
    ['backdo_enabled','백도',true,[true,false]], ['buk_mode_enabled','북',false,[true,false]],
    ['random_buk_destination','북 목적지 무작위',false,[true,false]],
    ['movement_order','결과 사용 순서','free',['free','fifo']],
    ['throw_timeout_seconds','던지기 시간(초)',20,[10,20,30]],
    ['move_timeout_seconds','이동 시간(초)',90,[30,60,90,120,150]],
  ];
  const settingLabels = {true:'사용',false:'사용 안 함',always:'항상',do_to_geol_plus_special:'도·개·걸·백도·북',none:'없음',selectable:'선택',forced:'강제',free:'자유',fifo:'나온 순서대로'};
  for (const [key,label,initial,values] of settingDefinitions) {
    const field = make('label',null,label); const select = make('select',`setting-${key}`);
    for(const value of values) {const option = make('option',null,settingLabels[String(value)] ?? String(value)); option.value=String(value); select.append(option);}
    select.value=String(initial); field.append(select); settingsPanel.append(field); settingsFields.set(key,{select,initial});
  }
  roomCreateForm.append(settingsPanel);
  function settings() {
    return Object.fromEntries([...settingsFields].map(([key,{select,initial}]) => [key,typeof initial === 'number' ? Number(select.value) : typeof initial === 'boolean' ? select.value === 'true' : select.value]));
  }
  views.room.append(roomDetail);
  const roster = make('aside', 'room-roster');
  const roomGrid = make('div'); roomGrid.className = 'room-grid';
  roomDetail.append(roomGrid); roomGrid.append(roster, roomMembers);
  const gameLayout = make('div'); gameLayout.className = 'game-layout';
  const boardColumn = make('div');
  const sidebar = make('aside'); sidebar.className = 'game-sidebar';
  views.game.append(gameLayout); gameLayout.append(boardColumn, sidebar);
  boardColumn.append(stage);
  sidebar.append(gameSession, throwYut);
  const targets = make('div', 'piece-targets'); stage.append(targets);
  const paths = document.createElementNS('http://www.w3.org/2000/svg','svg');
  paths.id = 'move-paths'; paths.setAttribute('viewBox','0 0 720 720');
  paths.setAttribute('aria-label','서버가 계산한 이동 경로'); stage.insertBefore(paths,targets);
  const turn = make('div', 'turn-description');
  const clock = make('div', 'turn-clock');
  const participants = make('ul', 'game-participants');
  const latest = make('div', 'latest-result', '아직 던진 결과가 없습니다.');
  latest.setAttribute('role', 'status');
  const queue = make('ol', 'result-queue'); queue.setAttribute('aria-label', '남은 윷 결과');
  const history = make('ol', 'throw-history'); history.setAttribute('aria-label', '최근 던지기');
  const waiting = make('div', 'waiting-pieces'); waiting.className = 'waiting-pieces';
  gameSession.prepend(turn, clock, participants, latest, queue, history);
  boardColumn.append(waiting, moveCandidates, finishedPieces);
  const leave = make('button', 'game-leave', '방 나가기'); leave.type = 'button';
  leave.addEventListener('click', () => sendRoomLobbyCommand('LEAVE_ROOM'));
  sidebar.append(leave);
  let snapshot = null;
  let selectedPiece = null;
  let matchKey = null;
  let resultSequence = 0;
  const recentResults = [];
  let timer = null;
  const resultName = BukScreenModel.resultName;
  const nickname = id => snapshot?.participants.find(p => p.user_id === id)?.nickname ?? id;
  function resultImage(result) {
    const img = make('img'); img.src = `assets/yut/result_${result}.png`; img.alt = resultName(result);
    img.addEventListener('error', () => { img.hidden = true; }); return img;
  }
  function resetMatch() {
    snapshot = null; selectedPiece = null; matchKey = null; resultSequence = 0;
    latest.replaceChildren(document.createTextNode('아직 던진 결과가 없습니다.'));
    recentResults.length = 0;
    history.replaceChildren(); queue.replaceChildren(); targets.replaceChildren(); waiting.replaceChildren(); paths.replaceChildren();
    if (timer !== null) clearInterval(timer); timer = null;
  }
  function sync() {
    const next = BukScreenModel.screen(authenticatedUserId !== null, activeRoomId, stateReconnectScope?.matchId);
    if (main.dataset.screen !== next) {
      profileModal.hidden = true; publicProfileModal.hidden = true; pauseModal.hidden = true;
      gameProfileMenu.open = false;
      if (next !== 'game') resetMatch();
    }
    main.dataset.screen = next;
    for (const [name, view] of Object.entries(views)) view.hidden = name !== next;
    chat.hidden = next === 'login';
    if (next === 'game') boardColumn.append(chat);
    else if (next !== 'login') views[next].append(chat);
    main.dataset.diagnostics = String(new URLSearchParams(location.search).has('diagnostics'));
    diagnostics.hidden = main.dataset.diagnostics !== 'true';
  }
  function room(detail) {
    try { sessionStorage.setItem(`buk-room:${authenticatedUserId}`,detail.summary.room_id); } catch { /* storage may be disabled */ }
    roster.replaceChildren(make('h3', null, '참가자'));
    for (const role of ['player','spectator']) {
      if (role === 'spectator') roster.append(make('h3', null, '관전자'));
      for (const member of detail.members.filter(m => m.role === role)) {
        roster.append(make('p', null, `${member.nickname}${member.team ? ` · ${member.team}팀` : ''}${member.is_cpu ? ' · CPU' : ''}`));
      }
    }
    sync();
  }
  async function authenticated() {
    sync();
    const userId = authenticatedUserId;
    try {
      const response = await fetch('/api/v1/profile/me',{credentials:'same-origin',cache:'no-store'});
      if (authenticatedUserId !== userId) return;
      if (response.ok) {
        const profile = await response.json();
        if (validatePrivateProfile(profile) && profile.user_id === userId) authStatus.textContent = `${profile.nickname} · ${profile.wins}승 ${profile.losses}패`;
      }
    } catch { /* keep authentication status; profile dialog offers retry */ }
    let remembered;
    try { remembered = sessionStorage.getItem(`buk-room:${userId}`); } catch { return; }
    if (!remembered || authenticatedUserId !== userId || activeRoomId) return;
    activeRoomId = remembered;
    if (!await refreshActiveRoomDetail() && authenticatedUserId === userId && activeRoomId === remembered) {
      activeRoomId = null;
      try { sessionStorage.removeItem(`buk-room:${userId}`); } catch { /* optional storage */ }
      sync();
    }
  }
  function forgetRoom() {
    try { sessionStorage.removeItem(`buk-room:${authenticatedUserId}`); } catch { /* optional storage */ }
  }
  function canControl() {
    const viewer = snapshot?.participants.find(p => p.user_id === authenticatedUserId);
    return !!viewer && snapshot.status === 'active' && !snapshot.pause.paused
      && viewer.role === 'player' && viewer.user_id === snapshot.current_turn.player_id
      && viewer.permissions.includes('control_game') && !viewer.cpu_control.active
      && canSendStateChangingCommand() && pendingMoveCommands.size === 0 && pendingRouteCommands.size === 0;
  }
  function choose(pieceId, routeOnly = false) {
    if (!canControl()) return;
    selectedPiece = pieceId;
    const candidates = snapshot.current_turn.move_request?.candidates.filter(c => c.piece_id === pieceId) ?? [];
    moveCandidates.replaceChildren();
    paths.replaceChildren();
    const piece = snapshot.pieces.find(p => p.piece_id === pieceId);
    const position = space => [Module.ccall('BukClientSpaceLogicalX','number',['string'],[space]), Module.ccall('BukClientSpaceLogicalY','number',['string'],[space])];
    for (const candidate of candidates) {
      const token = snapshot.result_queue.find(t => t.token_id === candidate.token_id);
      const button = make('button', null, `${resultName(token?.result)}로 이동`); button.type = 'button';
      button.addEventListener('click', () => { if (canControl()) sendMoveCandidate(candidate); });
      if (!routeOnly) moveCandidates.append(button);
      for (const preview of candidate.previews ?? []) {
        const start = piece.current_space_id ?? 'chammeogi';
        const points = [start, ...preview.traversed].map(position);
        const line = document.createElementNS(paths.namespaceURI,'polyline');
        line.setAttribute('points', points.map(p=>p.join(',')).join(' '));
        line.setAttribute('fill','none'); line.setAttribute('stroke',preview.route === 'shortcut' ? '#bd5516' : '#167f71');
        line.setAttribute('stroke-width','8'); line.setAttribute('stroke-linejoin','round'); line.setAttribute('opacity','.8');
        paths.append(line);
        const endpoint = preview.destination_space_id ? position(preview.destination_space_id) : points.at(-1);
        const dot = document.createElementNS(paths.namespaceURI,'circle');
        dot.setAttribute('cx',endpoint[0]); dot.setAttribute('cy',endpoint[1]); dot.setAttribute('r','17');
        dot.setAttribute('fill','none'); dot.setAttribute('stroke',line.getAttribute('stroke')); dot.setAttribute('stroke-width','5');
        paths.append(dot);
        const label = make('span',null,`${resultName(token?.result)} · ${preview.route === 'shortcut' ? '지름길' : preview.route === null ? '백도' : '바깥길'} · ${preview.destination_state === 'finished' ? '완주' : `${preview.traversed.length}칸 이동`}`);
        moveCandidates.append(label);
      }
    }
    for (const node of targets.children) node.setAttribute('aria-pressed', String(node.dataset.pieceId === selectedPiece));
  }
  function pieces() {
    targets.replaceChildren(); waiting.replaceChildren(); moveCandidates.replaceChildren(); paths.replaceChildren();
    const candidates = snapshot.current_turn.move_request?.candidates ?? [];
    const selectable = snapshot.current_turn.required_input === 'select_move' && canControl();
    for (let index = 0; index < snapshot.pieces.length; index++) {
      const piece = snapshot.pieces[index];
      if (piece.state === 'finished') continue;
      const button = make('button'); button.type = 'button';
      button.dataset.pieceId = piece.piece_id;
      button.setAttribute('aria-label', `${piece.team_id}팀 말 ${piece.piece_id}${piece.state === 'waiting' ? ' 출발 대기' : ''}`);
      button.disabled = !selectable || !candidates.some(c => c.piece_id === piece.piece_id);
      button.addEventListener('click', () => choose(piece.piece_id));
      if (piece.state === 'waiting') {
        button.className = 'waiting-piece';
        const img = make('img'); img.src = `assets/piece/${piece.team_id.toLowerCase()}_waiting.png`; img.alt = '';
        button.append(img, document.createTextNode(`${piece.team_id}팀 ${piece.piece_id}`)); waiting.append(button);
      } else if (wasmRuntimeReady) {
        const x = Module.ccall('BukClientPieceLogicalX', 'number', ['number'], [index]);
        const y = Module.ccall('BukClientPieceLogicalY', 'number', ['number'], [index]);
        if (x < 0 || y < 0) continue;
        button.className = 'piece-target'; button.style.left = `${x / 7.2}%`; button.style.top = `${y / 7.2}%`;
        button.textContent = piece.piece_id; targets.append(button);
      }
    }
    if (snapshot.current_turn.required_input === 'select_route') {
      if (candidates[0]) choose(candidates[0].piece_id, true);
      for (const route of candidates[0]?.routes ?? []) {
        const button = make('button', null, route === 'normal' ? '바깥길로 이동' : '지름길로 이동'); button.type = 'button';
        button.disabled = !canControl();
        button.addEventListener('click', () => {
          if (!canControl()) return;
          Module.ccall('BukClientRequestRouteSelection','number',['string'],[route]); drainRouteSelectionIntent();
        }); moveCandidates.append(button);
      }
    }
  }
  function match(value) {
    sync();
    const key = `${value.room_id}/${value.match_id}`;
    if (matchKey !== key) { if (matchKey !== null) resetMatch(); matchKey = key; }
    snapshot = value; selectedPiece = null;
    const current = value.participants.find(p => p.user_id === value.current_turn.player_id);
    const phase = {throw:'윷을 던지세요', select_move:'이동할 말을 선택하세요', select_route:'이동 경로를 선택하세요', none:'진행 중'}[value.current_turn.required_input];
    turn.textContent = `${current?.nickname ?? '경기'}${current?.team_id ? ` · ${current.team_id}팀` : ''} — ${value.pause.paused ? '일시정지' : current?.cpu_control.active ? 'CPU 진행 중' : phase}`;
    participants.replaceChildren();
    for (const p of value.participants) {
      const row = make('li', null, `${p.nickname}${p.user_id === authenticatedUserId ? ' (나)' : ''}${p.team_id ? ` · ${p.team_id}팀` : ' · 관전'}${p.cpu_control.active ? ' · CPU' : ''}${p.connected || p.cpu_control.reason === 'lobby_player' ? '' : ' · 연결 끊김'}`);
      if (p.user_id === value.current_turn.player_id) row.className = 'current-player';
      participants.append(row);
    }
    queue.replaceChildren();
    for (const token of value.result_queue) {
      const item = make('li'); item.className = 'result-token';
      item.append(resultImage(token.result), document.createTextNode(resultName(token.result))); queue.append(item);
    }
    if (!value.result_queue.length) queue.append(make('li', null, '남은 결과 없음'));
    if (timer !== null) clearInterval(timer);
    const updateClock = () => { clock.textContent = `남은 시간 ${Math.ceil(BukScreenModel.remaining(value.current_turn.timer) / 1000)}초`; };
    updateClock(); timer = setInterval(updateClock, 200);
    pieces();
  }
  function event(message) {
    if (message.type !== 'YUT_RESULT' || !RESULTS.has(message.payload?.token?.result)
      || typeof message.payload.player_id !== 'string' || message.sequence <= resultSequence) return;
    resultSequence = message.sequence;
    const {token, player_id: playerId} = message.payload;
    const label = `${playerId === authenticatedUserId ? '내 결과' : `${nickname(playerId)}의 결과`}: ${resultName(token.result)}`;
    recentResults.push({result: token.result, label});
    while (recentResults.length > 4) recentResults.shift();
    latest.replaceChildren();
    for (const recent of recentResults) {
      const item = make('span', null, recent.label); item.className = 'latest-result-entry';
      item.prepend(resultImage(recent.result)); latest.append(item);
    }
    history.append(make('li', null, label));
    while (history.children.length > 20) history.firstElementChild.remove();
    history.scrollTop = history.scrollHeight;
  }
  sync();
  return {sync, room, match, event, resetMatch, settings, authenticated, forgetRoom};
})();
