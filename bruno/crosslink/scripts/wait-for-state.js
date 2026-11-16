// Pull-slip output queues an action; the HTTP response does not wait for it.
module.exports = async function waitForState(bru, req, res, expectedState, timeoutMs = 5000) {
  if (res.getStatus() !== 200) {
    throw new Error(`Expected HTTP 200 while waiting for ${expectedState}, got ${res.getStatus()}`);
  }

  const deadline = Date.now() + timeoutMs;
  let request = res.getBody();
  while (request.state !== expectedState && Date.now() < deadline) {
    await bru.sleep(100);
    const remaining = deadline - Date.now();
    if (remaining <= 0) break;

    const response = await bru.sendRequest({
      method: "GET",
      url: req.getUrl(),
      headers: req.getHeaders(),
      auth: {
        username: bru.getEnvVar("userName"),
        password: bru.getEnvVar("userPassword")
      },
      timeout: remaining
    });
    if (response.status !== 200) {
      throw new Error(`Expected HTTP 200 while waiting for ${expectedState}, got ${response.status}`);
    }
    request = response.data;
  }

  if (request.state !== expectedState) {
    throw new Error(`Timed out waiting for ${expectedState}; last state: ${request.state}`);
  }
  return request;
};
