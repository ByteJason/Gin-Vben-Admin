import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

const root = join(import.meta.dirname, "..", "..");
const managementUIs = ["web-antd", "web-ele", "web-naive"];

function read(path) {
  return readFileSync(join(root, path), "utf8");
}

test("shared image captcha component renders the server challenge and keeps its id", () => {
  const component = read(
    "admin/packages/effects/common-ui/src/components/captcha/image-captcha/index.vue",
  );
  const types = read("admin/packages/effects/common-ui/src/components/captcha/types.ts");

  assert.match(component, /defineModel<string>/, "captcha answer is a form model");
  assert.match(
    types,
    /request:\s*ImageCaptchaRequest/,
    "component accepts the versioned challenge request",
  );
  assert.match(types, /onChallengeId\??:/, "component exposes the challenge id seam");
  assert.match(component, /props\.request/, "component invokes the challenge request");
  assert.match(component, /payload/, "server image payload is rendered");
  assert.match(component, /expiresIn/, "challenge expiry is retained for the UI");
  assert.match(component, /data-testid=["']image-captcha["']/, "stable UI test target");
  assert.match(component, /type=["']button["']/, "refresh control is keyboard actionable");
});

test("all management login forms consume image challenges and submit captchaId", () => {
  for (const ui of managementUIs) {
    const login = read(`admin/apps/${ui}/src/views/_core/authentication/login.vue`);
    const store = read(`admin/apps/${ui}/src/store/auth.ts`);
    const loginCall = store.slice(
      store.indexOf("loginApi({"),
      store.indexOf("});", store.indexOf("loginApi({")) + 3,
    );

    assert.match(login, /ImageCaptcha/, `${ui} image captcha component`);
    assert.match(login, /getCaptchaApi/, `${ui} challenge endpoint`);
    assert.match(login, /const captchaId = ref/, `${ui} challenge id state`);
    assert.match(
      login,
      /:\s*z\.string\(\)\.optional\(\)/,
      `${ui} keeps the default-off risk policy server-driven`,
    );
    assert.match(
      login,
      /captchaId\.value\s*\?\s*z\s*\.string\(\)\s*\.trim\(\)\s*\.min\(1,/,
      `${ui} requires an answer after an image challenge is displayed`,
    );
    assert.match(login, /captchaId:\s*captchaId\.value/, `${ui} submit challenge id`);
    assert.doesNotMatch(login, /SliderCaptcha/, `${ui} does not use the deferred slider provider`);
    assert.match(
      store,
      /captchaId:\s*typeof params\.captchaId === ['"]string['"]/,
      `${ui} store forwards captchaId`,
    );
    assert.match(loginCall, /captchaId/, `${ui} login API receives captchaId`);
  }
});
