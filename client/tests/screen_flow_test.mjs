import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';


const context = vm.createContext({});
vm.runInContext(readFileSync(new URL('../web/screen-model.js', import.meta.url), 'utf8'), context);
const model = context.BukScreenModel;
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
