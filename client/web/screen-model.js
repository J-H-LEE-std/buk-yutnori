/* Display-only helpers. All game decisions remain on the server. */
globalThis.BukScreenModel = Object.freeze({
  screen(authenticated, roomId, matchId) {
    return !authenticated ? 'login' : !roomId ? 'lobby' : matchId ? 'game' : 'room';
  },
  resultName(result) {
    return ({do:'도', gae:'개', geol:'걸', yut:'윷', mo:'모', backdo:'백도', buk:'북'})[result]
      ?? '알 수 없는 결과';
  },
  remaining(timer, now = Date.now()) {
    const deadline = timer.deadline_at === null ? NaN : Date.parse(timer.deadline_at);
    return Number.isFinite(deadline) ? Math.max(0, deadline - now) : timer.remaining_ms;
  },
});
