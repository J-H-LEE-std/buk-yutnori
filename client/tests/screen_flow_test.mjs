import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';


const context = vm.createContext({});
vm.runInContext(readFileSync(new URL('../web/screen-model.js', import.meta.url), 'utf8'), context);
const model = context.BukScreenModel;
const shell = readFileSync(new URL('../web/shell.html', import.meta.url), 'utf8');
assert.match(shell, /<main data-screen="login">/,
  'the login view must be the initial fail-closed screen before session bootstrap');
assert.match(shell, /main\[data-screen="login"\]\s*>\s*:not\(\.auth\):not\(\.screen-shell\)\s*\{\s*display:\s*none\s*!important;/,
  'unauthenticated app content must stay hidden until screen initialization');
assert.match(shell, /id="auth-retry"[^>]*hidden[^>]*>다시 연결<\/button>/,
  'authentication timeout must offer an in-app reconnect action');
assert.match(shell, /authRetry\.addEventListener\("click",\s*\(\)\s*=>\s*\{\s*prepareGoogleIdentityServicesRetry\(\);\s*void initializeAuth\(\);\s*\}\)/,
  'the reconnect action must reset a timed-out Google script request and restart authentication bootstrap');
assert.match(shell, /로그인 연결 시간이 초과됐습니다\. 다시 연결할까요\?/,
  'timeout messaging must explain the reconnect choice');
assert.match(shell, /if \(!configResponse\.ok\)\s*\{\s*throw new Error\(`auth config failed:/,
  'failed auth configuration requests must offer the retry path');
assert.equal(model.screen(false, 'room', 'match'), 'login');
assert.equal(model.screen(true, null, null), 'lobby');
assert.equal(model.screen(true, 'room', null), 'room');
assert.equal(model.screen(true, 'room', 'match'), 'game');
assert.equal(model.resultName('gae'), '개');
assert.equal(model.resultName('backdo'), '백도');
assert.equal(model.resultName('invented'), '알 수 없는 결과');
assert.equal(model.remaining({remaining_ms: 9000, deadline_at: null}, 500), 9000);
assert.equal(model.remaining({remaining_ms: 9000, deadline_at: '1970-01-01T00:00:02Z'}, 500), 1500);
assert.equal(model.remaining({remaining_ms: 9000, deadline_at: '1970-01-01T00:00:02Z'}, 2500), 0);
console.log('SCREEN_MODEL_OK');
