const assert = require("node:assert/strict");
const test = require("node:test");
const waitForState = require("./wait-for-state");

const req = {
  getUrl: () => "http://broker/patron_requests/loan?side=lending",
  getHeaders: () => ({ "X-Okapi-Tenant": "supplier" })
};
const response = state => ({ getStatus: () => 200, getBody: () => ({ state }) });

test("returns immediately when the requested state is already visible", async () => {
  assert.deepEqual(await waitForState({}, req, response("SEARCHING"), "SEARCHING"), { state: "SEARCHING" });
});

test("polls the same tenant and credentials until the queued action completes", async () => {
  const calls = [];
  const bru = {
    sleep: async () => {},
    getEnvVar: name => ({ userName: "staff", userPassword: "test-password" })[name],
    sendRequest: async config => {
      calls.push(config);
      return { status: 200, data: { state: calls.length === 2 ? "SEARCHING" : "WILL_SUPPLY" } };
    }
  };
  const result = await waitForState(bru, req, response("WILL_SUPPLY"), "SEARCHING");
  assert.equal(result.state, "SEARCHING");
  assert.equal(calls.length, 2);
  for (const call of calls) {
    assert.equal(call.method, "GET");
    assert.equal(call.url, req.getUrl());
    assert.deepEqual(call.headers, req.getHeaders());
    assert.deepEqual(call.auth, { username: "staff", password: "test-password" });
    assert.ok(call.timeout > 0 && call.timeout <= 5000);
  }
});

test("fails within the deadline when the action never completes", async t => {
  let now = 0;
  t.mock.method(Date, "now", () => now);
  let polls = 0;
  const bru = {
    sleep: async ms => { now += ms; },
    getEnvVar: () => "test",
    sendRequest: async () => {
      polls++;
      return { status: 200, data: { state: "WILL_SUPPLY" } };
    }
  };
  await assert.rejects(waitForState(bru, req, response("WILL_SUPPLY"), "SEARCHING", 500),
    /Timed out waiting for SEARCHING; last state: WILL_SUPPLY/);
  assert.equal(now, 500);
  assert.equal(polls, 4);
});

test("propagates HTTP failures instead of retrying them as pending states", async () => {
  const failure = new Error("HTTP 500");
  const bru = {
    sleep: async () => {},
    getEnvVar: () => "test",
    sendRequest: async () => { throw failure; }
  };
  await assert.rejects(waitForState(bru, req, response("WILL_SUPPLY"), "SEARCHING"), failure);
});
