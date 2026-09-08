"use strict";

const { request } = require("playwright-core");

let input = "";
process.stdin.setEncoding("utf8");
process.stdin.on("data", chunk => { input += chunk; });
process.stdin.on("end", async () => {
  let api;
  try {
    const options = JSON.parse(input);
    if (!Array.isArray(options.urls) || options.urls.length === 0) {
      throw new Error("no ActiveSG API URLs supplied");
    }
    for (const rawURL of options.urls) {
      const parsed = new URL(rawURL);
      if (parsed.protocol !== "https:" || parsed.hostname !== "activesg.gov.sg" ||
          !parsed.pathname.startsWith("/api/trpc/")) {
        throw new Error("refusing non-ActiveSG API URL");
      }
    }
    api = await request.newContext({
      storageState: options.storageState,
      timeout: options.timeoutMilliseconds,
    });
    const responses = [];
    for (const rawURL of options.urls) {
      const response = await api.get(rawURL);
      responses.push({
        status: response.status(),
        statusText: response.statusText(),
        body: await response.text(),
      });
    }
    process.stdout.write(JSON.stringify(responses));
  } catch (error) {
    process.stderr.write(String(error && error.message ? error.message : error));
    process.exitCode = 1;
  } finally {
    if (api) await api.dispose();
  }
});
