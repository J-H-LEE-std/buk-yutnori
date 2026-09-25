import assert from 'node:assert/strict';
import { connect } from './devtools.mjs';

const [debuggerURL = 'http://localhost:9231', pageURL = 'http://localhost:8766/'] = process.argv.slice(2);
const targets = await (await fetch(`${debuggerURL}/json/list`)).json();
const target = targets.find(item => item.type === 'page' && item.url.startsWith(pageURL));
assert(target, 'test page must be open');
const socket = await connect(target.webSocketDebuggerUrl);
let nextID = 1;
const requests = new Map();
const runtimeErrors = [];
socket.onMessage = text => {
  const message = JSON.parse(text);
  const request = requests.get(message.id);
  if (!request) {
    if (message.method === 'Runtime.exceptionThrown') runtimeErrors.push(message.params.exceptionDetails);
    if (message.method === 'Log.entryAdded' && message.params.entry.level === 'error') runtimeErrors.push(message.params.entry);
    return;
  }
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
  throw new Error(`Timed out: ${expression}\n${await evaluate('document.body.innerText')}\nBrowser errors: ${JSON.stringify(runtimeErrors)}`);
}

try {
  await call('Page.enable');
  await call('Runtime.enable');
  await call('Log.enable');
  await call('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true });
  await call('Network.clearBrowserCookies');
  const timeoutStartedAt = Date.now();
  const timeoutProbe = await call('Page.addScriptToEvaluateOnNewDocument', { source: `{
    const nativeFetch = globalThis.fetch.bind(globalThis);
    globalThis.__bukNativeFetch = nativeFetch;
    globalThis.__bukAuthProbe = { stallSession: true, configFailures: 1, loginFailures: 1 };
    globalThis.google = { accounts: { id: {
      initialize() {},
      renderButton(element) { element.dataset.testGoogleButton = 'ready'; },
    } } };
    globalThis.fetch = (input, options = {}) => {
      const url = String(input);
      if (url.includes('/api/v1/auth/session') && globalThis.__bukAuthProbe.stallSession) {
        const body = new ReadableStream({ start(controller) {
          const abort = () => {
            globalThis.__bukAuthProbe.stallSession = false;
            controller.error(new DOMException('Aborted', 'AbortError'));
          };
          if (options.signal?.aborted) abort();
          else options.signal?.addEventListener('abort', abort, { once: true });
        } });
        return Promise.resolve(new Response(body, { status: 200, headers: { 'Content-Type': 'application/json' } }));
      }
      if (url.includes('/api/v1/auth/config') && globalThis.__bukAuthProbe.configFailures > 0) {
        globalThis.__bukAuthProbe.configFailures -= 1;
        return Promise.resolve(new Response('', { status: 503 }));
      }
      if (url.includes('/api/v1/auth/google') && globalThis.__bukAuthProbe.loginFailures > 0) {
        globalThis.__bukAuthProbe.loginFailures -= 1;
        return Promise.resolve(new Response('', { status: 503 }));
      }
      return nativeFetch(input, options);
    };
  }` });
  await call('Page.navigate', { url: pageURL });
  await wait('typeof wasmRuntimeReady !== "undefined" && wasmRuntimeReady && typeof BukScreens !== "undefined"');
  await wait('!document.querySelector("#auth-retry").hidden');
  assert(Date.now() - timeoutStartedAt >= 11000, 'session-body stall must remain pending until the 12-second timeout');
  assert.equal(await evaluate('document.querySelector("#auth-status").textContent'),
    '로그인 연결 시간이 초과됐습니다. 다시 연결할까요?');
  assert.equal(await evaluate('document.querySelector("main").dataset.screen'), 'login');
  assert.equal(await evaluate('document.querySelector("#screen-lobby").hidden'), true,
    'an unauthenticated mobile visitor must not see the lobby');
  assert(await evaluate('document.querySelector(".auth").getClientRects().length > 0'),
    'mobile login must remain visible while authentication is unavailable');

  await evaluate('document.querySelector("#auth-retry").click()');
  await wait('!document.querySelector("#auth-retry").hidden');
  assert.equal(await evaluate('document.querySelector("#auth-status").textContent'),
    '로그인 연결에 실패했습니다. 다시 연결할까요?', 'config 503 must provide an in-app retry');
  await evaluate('document.querySelector("#auth-retry").click()');
  await wait('document.querySelector("#auth-status").textContent === "Google 로그인이 필요합니다."');
  assert.equal(await evaluate('document.querySelector("#auth-retry").hidden'), true,
    'retry control must disappear once login is ready');
  await evaluate('handleGoogleCredential({ credential: "browser-test-credential" })');
  assert.equal(await evaluate('document.querySelector("#auth-status").textContent'),
    '로그인 연결에 실패했습니다. 다시 연결할까요?', 'login API 503 must provide an in-app retry');
  assert.equal(await evaluate('document.querySelector("#auth-retry").hidden'), false);
  await evaluate('document.querySelector("#auth-retry").click()');
  await wait('document.querySelector("#auth-status").textContent === "Google 로그인이 필요합니다."');
  await evaluate('handleGoogleCredential({ credential: "browser-test-player-auth-retry" })');
  await wait('document.querySelector("#logout").hidden === false');
  assert.equal(await evaluate('document.querySelector("#auth-retry").hidden'), true,
    'successful Google sign-in must hide a retry control left by a transient failure');
  const gsiLoad = await evaluate(`(async () => {
    delete window.google;
    googleIdentityServicesPromise = null;
    googleIdentityServicesInitialized = false;
    let appendCount = 0;
    let loadingScript;
    const appendChild = document.head.appendChild.bind(document.head);
    document.head.appendChild = element => {
      if (element.src === "https://accounts.google.com/gsi/client") {
        appendCount += 1;
        loadingScript = element;
        return element;
      }
      return appendChild(element);
    };
    try {
      const first = loadGoogleIdentityServices();
      const second = loadGoogleIdentityServices();
      const samePromise = first === second;
      await Promise.resolve();
      loadingScript.dispatchEvent(new Event("load"));
      await Promise.all([first, second]);
      return { appendCount, samePromise };
    } finally {
      document.head.appendChild = appendChild;
    }
  })()`);
  assert.deepEqual(gsiLoad, { appendCount: 1, samePromise: true },
    'concurrent GSI retries must share one script-load promise');
  const gsiInitCount = await evaluate(`(async () => {
    let initializeCount = 0;
    window.google = { accounts: { id: {
      initialize() { initializeCount += 1; },
      renderButton() {},
    } } };
    await initializeGoogleButton();
    await initializeGoogleButton();
    return initializeCount;
  })()`);
  assert.equal(gsiInitCount, 1, 'GSI must initialize only once across auth retries');
  await call('Page.removeScriptToEvaluateOnNewDocument', { identifier: timeoutProbe.identifier });
  console.log('AUTH_BOOTSTRAP_BROWSER_OK body stall -> config 503 -> login API 503 -> retry');
} finally {
  socket.close();
}
