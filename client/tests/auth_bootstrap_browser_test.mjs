import assert from 'node:assert/strict';
import { connect } from './devtools.mjs';

const [debuggerURL = 'http://localhost:9231', pageURL = 'http://localhost:8766/'] = process.argv.slice(2);
const targets = await (await fetch(`${debuggerURL}/json/list`)).json();
const target = targets.find(item => item.type === 'page' && item.url.startsWith(pageURL));
assert(target, 'test page must be open');
const socket = await connect(target.webSocketDebuggerUrl);
let nextID = 1;
const requests = new Map();
socket.onMessage = text => {
  const message = JSON.parse(text);
  const request = requests.get(message.id);
  if (!request) return;
  requests.delete(message.id);
  if (message.error) request.reject(new Error(JSON.stringify(message.error)));
  else request.resolve(message.result);
};
const call = (method, params = {}) => new Promise((resolve, reject) => {
  const id = nextID++;
  requests.set(id, { resolve, reject });
  socket.send(JSON.stringify({ id, method, params }));
});
async function evaluate(expression) {
  const result = await call('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true });
  if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails));
  return result.result.value;
}
async function wait(expression, timeout = 20000) {
  const deadline = Date.now() + timeout;
  do {
    if (await evaluate(expression)) return;
    await new Promise(resolve => setTimeout(resolve, 100));
  } while (Date.now() < deadline);
  throw new Error(`Timed out: ${expression}\n${await evaluate('document.body.innerText')}`);
}

try {
  await call('Page.enable');
  await call('Runtime.enable');
  await call('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true });
  await call('Network.clearBrowserCookies');
  const timeoutProbe = await call('Page.addScriptToEvaluateOnNewDocument', { source: `{
    const nativeFetch = globalThis.fetch.bind(globalThis);
    globalThis.__bukNativeFetch = nativeFetch;
    globalThis.fetch = (input, options = {}) => {
      if (!String(input).includes('/api/v1/auth/session')) return nativeFetch(input, options);
      return new Promise((resolve, reject) => {
        const abort = () => reject(new DOMException('Aborted', 'AbortError'));
        if (options.signal?.aborted) abort();
        else options.signal?.addEventListener('abort', abort, { once: true });
      });
    };
  }` });
  await call('Page.navigate', { url: pageURL });
  await wait('typeof wasmRuntimeReady !== "undefined" && wasmRuntimeReady && typeof BukScreens !== "undefined"');
  await wait('!document.querySelector("#auth-retry").hidden');
  assert.equal(await evaluate('document.querySelector("#auth-status").textContent'),
    '로그인 연결 시간이 초과됐습니다. 다시 연결할까요?');
  assert.equal(await evaluate('document.querySelector("main").dataset.screen'), 'login');
  assert.equal(await evaluate('document.querySelector("#screen-lobby").hidden'), true,
    'an unauthenticated mobile visitor must not see the lobby');
  assert(await evaluate('document.querySelector(".auth").getClientRects().length > 0'),
    'mobile login must remain visible while authentication is unavailable');

  await evaluate('globalThis.fetch = globalThis.__bukNativeFetch');
  await evaluate('document.querySelector("#auth-retry").click()');
  await wait('document.querySelector("#auth-status").textContent === "Google 로그인이 필요합니다."');
  assert.equal(await evaluate('document.querySelector("#auth-retry").hidden'), true,
    'retry control must disappear once login is ready');
  await call('Page.removeScriptToEvaluateOnNewDocument', { identifier: timeoutProbe.identifier });
  console.log('AUTH_BOOTSTRAP_BROWSER_OK mobile timeout -> login retry');
} finally {
  socket.close();
}
