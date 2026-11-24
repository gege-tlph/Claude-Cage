// The release signing certificate in packaging/windows is the one the
// installer offers to trust and the one the Windows updater pins.
import assert from 'node:assert/strict';
import { createHash, X509Certificate } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const file = path => readFile(new URL(`../${path}`, import.meta.url));
const certificate = await file('packaging/windows/cage-lite-signing.cer');
const installer = (await file('packaging/windows/Cage.nsi')).toString('utf8');
const languages = (await file('packaging/windows/CageInstallerLanguages.nsh')).toString('utf8');
const authenticode = (await file('desktop/windows/Authenticode.cs')).toString('utf8');

test('the installer offers to trust the release certificate it carries', () => {
  const thumbprint = createHash('sha1').update(certificate).digest('hex').toUpperCase();
  assert.equal(installer.match(/^!define PUBLISHER_THUMBPRINT "([0-9A-F]{40})"$/m)?.[1], thumbprint);
  assert.match(installer, /File \/oname=cage-lite-signing\.cer "cage-lite-signing\.cer"/);
  for (const language of ['LANG_SIMPCHINESE', 'LANG_ENGLISH']) {
    assert.match(languages, new RegExp(`^LangString CageTrustPublisher \\$\\{${language}\\} "[^"]+"$`, 'm'));
  }
});

test('adding the certificate runs only when chosen, and visibly', () => {
  const step = installer.slice(installer.indexOf('${If} $TrustPublisher == 1'));
  assert.ok(step.length < installer.length, 'the trust step is not guarded by the choice');
  const command = step.slice(0, step.indexOf('${EndIf}'));
  // Windows confirms the new root in a dialog; a hidden process could keep it out of sight.
  assert.match(command, /ExecWait '"\$SYSDIR\\certutil\.exe" -user -addstore Root /);
  assert.doesNotMatch(command, /nsExec/);
});

test('the Windows updater pins the same certificate key', () => {
  const key = new X509Certificate(certificate).publicKey.export({ type: 'spki', format: 'der' });
  const pinned = authenticode.match(/OfficialPublisherKey = "([0-9A-F]{64})"/)?.[1];
  assert.equal(pinned, createHash('sha256').update(key).digest('hex').toUpperCase());
});
