// Editable drop-downs (系统时区, 界面字体, 代码字体): typing a whole option's
// value leaves only that row instead of every option, Enter keeps the text and
// closes the list, and a click on the field still lists every option.
import assert from "node:assert/strict";
import { createWorkbench } from "./workbench-fixture.mjs";

const { browser, page, state, errors } = await createWorkbench();
const options = (name) => page.getByRole("listbox", { name: name + "选项" }).getByRole("option");

async function check(name, typedValue) {
  const field = page.getByRole("combobox", { name, exact: true });
  await field.click();
  const all = await options(name).count();
  assert.ok(all > 1, `${name}: a click lists ${all} options`);
  await field.fill("");
  await field.pressSequentially(typedValue);
  await page.waitForFunction(([label, value]) => {
    const list = [...document.querySelectorAll("[role=listbox]")].find((l) => l.getAttribute("aria-label") === label + "选项");
    return list && [...list.querySelectorAll("[role=option]")].length === 1 && list.textContent.includes(value);
  }, [name, typedValue]);
  await field.press("Enter");
  await page.getByRole("listbox", { name: name + "选项" }).waitFor({ state: "detached" });
  assert.equal(await field.inputValue(), typedValue, `${name}: Enter changed the text`);
  await field.click();
  assert.equal(await options(name).count(), all, `${name}: a click after typing does not list every option`);
  await field.press("Escape");
}

try {
  state.settings.auto_timezone = false;
  await page.reload();
  await page.getByRole("button", { name: /^设置/ }).click();
  await page.getByRole("button", { name: "时区与语言", exact: true }).click();
  await check("系统时区", "Asia/Tokyo");

  await page.getByRole("button", { name: "外观", exact: true }).click();
  await page.getByRole("button", { name: "自定义", exact: true }).first().click();
  const ui = page.getByRole("combobox", { name: /界面字体$/ }).first();
  const code = page.getByRole("combobox", { name: /代码字体$/ }).first();
  await check(await ui.getAttribute("aria-label"), "Arial");
  await check(await code.getAttribute("aria-label"), await code.evaluate(async (input) => {
    input.click();
    await new Promise((resolve) => setTimeout(resolve, 100));
    const first = document.querySelector("[role=listbox] [role=option]")?.getAttribute("data-value");
    input.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    return first;
  }));
  assert.deepEqual(errors, []);
  console.log("editable inline select smoke passed");
} finally {
  await browser.close();
}
