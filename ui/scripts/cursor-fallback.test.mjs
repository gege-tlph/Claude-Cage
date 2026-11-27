import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';

// Loads cursor.js with the browser's answers given: whether it reports touch,
// and whether its main pointer is fine (a mouse or touchpad).
async function loadCursor({ touch = false, finePointer = true } = {}) {
  const source = (await readFile(new URL('../vendor/novnc/core/util/cursor.js', import.meta.url), 'utf8'))
    .replace(/^import .*;$/m, `const supportsCursorURIs = true, isTouchDevice = ${touch};`)
    .replace('export default class Cursor', 'globalThis.Cursor = class Cursor');
  const context = vm.createContext({
    document: {
      createElement: () => ({ style: {}, width: 0, height: 0,
        getContext: () => ({ clearRect() {}, putImageData() {} }),
        toDataURL: () => 'data:image/png;base64,test' }),
      body: { appendChild() {}, removeChild() {} },
      contains: () => true,
    },
    matchMedia: (query) => ({ matches: query === '(pointer: fine)' && finePointer }),
    ImageData: class {}, Uint8ClampedArray,
  });
  vm.runInContext(source, context);
  return context.Cursor;
}

const target = () => ({ style: {}, addEventListener() {}, removeEventListener() {} });

test('empty cursor uses opt-in fallback and real cursor shapes still win', async () => {
  const Cursor = await loadCursor();
  for (const fallback of ['default', 'none']) {
    const t = target();
    const cursor = new Cursor(fallback);
    cursor.attach(t);
    assert.equal(t.style.cursor, fallback);
    cursor.change(new Uint8Array([0, 0, 0, 255]), 0, 0, 1, 1);
    assert.match(t.style.cursor, /^url\(/);
    cursor.change(new Uint8Array(), 0, 0, 0, 0);
    assert.equal(t.style.cursor, fallback);
  }
});

// A laptop with a touch screen: the touchpad moves the system cursor, which
// shows the guest's shape itself; a drawn one would trail next to it.
test('a touch screen with a fine main pointer keeps the system cursor', async () => {
  const Cursor = await loadCursor({ touch: true, finePointer: true });
  const t = target();
  const cursor = new Cursor('default');
  cursor.attach(t);
  cursor.change(new Uint8Array([0, 0, 0, 255]), 0, 0, 1, 1);
  assert.match(t.style.cursor, /^url\(/);
});

// A phone or tablet: the guest's cursor is drawn, and never next to a
// system cursor that the browser display's 'default' fallback showed.
test('a drawn cursor hides the system one, an empty one shows the fallback', async () => {
  const Cursor = await loadCursor({ touch: true, finePointer: false });
  for (const fallback of ['default', 'none']) {
    const t = target();
    const cursor = new Cursor(fallback);
    cursor.attach(t);
    assert.equal(t.style.cursor, fallback);
    cursor.change(new Uint8Array([0, 0, 0, 255]), 0, 0, 1, 1);
    assert.equal(t.style.cursor, 'none');
    cursor.change(new Uint8Array(), 0, 0, 0, 0);
    assert.equal(t.style.cursor, fallback);
  }
});
