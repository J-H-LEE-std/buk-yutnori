import assert from 'node:assert/strict';
import { connect } from './devtools.mjs';

const [debuggerURL = 'http://localhost:9231', pageURL = 'http://localhost:8766/'] = process.argv.slice(2);
let target;
let lastTargetError;
const targetDeadline = Date.now() + 15000;
while (!target && Date.now() < targetDeadline) {
  try {
    const response = await fetch(`${debuggerURL}/json/list`);
    if (!response.ok) throw new Error(`DevTools target list returned ${response.status}`);
    const targets = await response.json();
    target = targets.find(item => item.type === 'page' && item.url.startsWith(pageURL));
  } catch (error) {
    lastTargetError = error;
  }
  if (!target) await new Promise(resolve => setTimeout(resolve, 100));
}
assert(target, `test page must be open: ${lastTargetError ?? 'not found'}`);
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
    globalThis.__bukAuthProbe = { stallSession: true, configFailures: 1, loginFailures: 1, loginRateLimits: 1 };
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
      if (url.includes('/api/v1/auth/google') && globalThis.__bukAuthProbe.loginRateLimits > 0) {
        globalThis.__bukAuthProbe.loginRateLimits -= 1;
        return Promise.resolve(new Response('', { status: 429 }));
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
  await evaluate('handleGoogleCredential({ credential: "browser-test-player-rate-limit" })');
  assert.equal(await evaluate('document.querySelector("#auth-retry").hidden'), false,
    'login rate limiting must offer an in-app retry');
  assert.equal(await evaluate('document.querySelector("#auth-status").textContent'),
    '로그인 연결에 실패했습니다. 다시 연결할까요?');
  await evaluate('document.querySelector("#auth-retry").click()');
  await wait('document.querySelector("#auth-status").textContent === "Google 로그인이 필요합니다."');
  await evaluate('handleGoogleCredential({ credential: "browser-test-player-auth-retry" })');
  await wait('document.querySelector("#logout").hidden === false');
  assert.equal(await evaluate('document.querySelector("#auth-retry").hidden'), true,
    'successful Google sign-in must hide a retry control left by a transient failure');
  const gsiLoad = await evaluate(`(async () => {
    delete window.google;
    googleIdentityServicesScriptPromise = null;
    googleIdentityServicesScriptElement = null;
    googleIdentityServicesScriptReject = null;
    googleIdentityServicesScriptTimedOut = false;
    initializedGoogleIdentityServicesId = null;
    let appendCount = 0;
    const loadingScripts = [];
    const appendChild = document.head.appendChild.bind(document.head);
    const setTimeout = window.setTimeout.bind(window);
    document.head.appendChild = element => {
      if (element.src === "https://accounts.google.com/gsi/client") {
        appendCount += 1;
        const loadingScript = { element, removed: false };
        loadingScripts.push(loadingScript);
        const remove = element.remove.bind(element);
        element.remove = () => {
          loadingScript.removed = true;
          remove();
        };
        return element;
      }
      return appendChild(element);
    };
    window.setTimeout = (callback, delay, ...args) => setTimeout(callback, Math.min(delay, 10), ...args);
    try {
      const first = loadGoogleIdentityServices();
      const second = loadGoogleIdentityServices();
      const initialResults = await Promise.all([first, second].map(promise =>
        promise.then(() => false, error => /timed out/i.test(error.message))));
      prepareGoogleIdentityServicesRetry();
      const firstScriptRemovedOnRetry = loadingScripts[0].removed;
      const retry = loadGoogleIdentityServices();
      loadingScripts[1].element.dispatchEvent(new Event("load"));
      await retry;
      return { appendCount, firstScriptRemovedOnRetry, initialResults, timedOutReset: !googleIdentityServicesScriptTimedOut };
    } finally {
      document.head.appendChild = appendChild;
      window.setTimeout = setTimeout;
    }
  })()`);
  assert.deepEqual(gsiLoad, {
    appendCount: 2,
    firstScriptRemovedOnRetry: true,
    initialResults: [true, true],
    timedOutReset: true,
  }, 'concurrent GSI requests must share one load and an explicit retry must replace a timed-out load');
  const gsiInitCount = await evaluate(`(async () => {
    let initialIdentityInitializeCount = 0;
    let replacementIdentityInitializeCount = 0;
    const initialIdentity = {
      initialize() { initialIdentityInitializeCount += 1; },
      renderButton() {},
    };
    const replacementIdentity = {
      initialize() { replacementIdentityInitializeCount += 1; },
      renderButton() {},
    };
    window.google = { accounts: { id: initialIdentity } };
    await initializeGoogleButton();
    await initializeGoogleButton();
    window.google.accounts.id = replacementIdentity;
    await initializeGoogleButton();
    delete window.google;
    const timedOutScript = document.createElement("script");
    document.head.appendChild(timedOutScript);
    let staleScriptRejectCount = 0;
    googleIdentityServicesScriptElement = timedOutScript;
    googleIdentityServicesScriptPromise = new Promise(() => {});
    googleIdentityServicesScriptReject = () => { staleScriptRejectCount += 1; };
    googleIdentityServicesScriptTimedOut = true;
    const authInitialization = initializeAuth();
    const timeoutClearedByAuthInitialization = !googleIdentityServicesScriptTimedOut;
    const staleScriptRemovedByAuthInitialization = !timedOutScript.isConnected
      && googleIdentityServicesScriptElement === null
      && googleIdentityServicesScriptPromise === null
      && googleIdentityServicesScriptReject === null
      && staleScriptRejectCount === 1;
    await authInitialization;
    delete googleSignin.dataset.testGoogleButton;
    return {
      initialIdentityInitializeCount,
      replacementIdentityInitializeCount,
      timeoutClearedByAuthInitialization,
      staleScriptRemovedByAuthInitialization,
    };
  })()`);
  assert.deepEqual(gsiInitCount, {
    initialIdentityInitializeCount: 1,
    replacementIdentityInitializeCount: 1,
    timeoutClearedByAuthInitialization: true,
    staleScriptRemovedByAuthInitialization: true,
  }, 'GSI must initialize once per runtime identity and auth reinitialization must clear stale timeouts');
  await call('Page.removeScriptToEvaluateOnNewDocument', { identifier: timeoutProbe.identifier });
  console.log('AUTH_BOOTSTRAP_BROWSER_OK body stall -> config 503 -> login 503/429 -> retry -> GSI retry');
} finally {
  socket.close();
}
