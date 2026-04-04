const { chromium } = require("playwright");

function isPinterestCookie(cookie) {
  if (!cookie || !cookie.domain) {
    return false;
  }
  return cookie.domain === "pinterest.com" || cookie.domain.endsWith(".pinterest.com");
}

function cookiesToHeader(cookies) {
  return cookies
    .filter(isPinterestCookie)
    .filter((cookie) => cookie && cookie.name && cookie.value)
    .map((cookie) => `${cookie.name}=${cookie.value}`)
    .join("; ");
}

(async () => {
  const browser = await chromium.launch({ headless: false });
  const context = await browser.newContext();
  const page = await context.newPage();

  let captured = false;
  let bookmark = "";
  let capturedHeaders = null;
  let capturedUserAgent = "";
  let capturedDataJSON = "";
  let capturedSourceURL = "";

  const headerAllowlist = new Set([
    "accept",
    "accept-language",
    "origin",
    "referer",
    "user-agent",
    "x-csrftoken",
    "x-requested-with",
    "x-pinterest-source-url",
    "sec-ch-ua",
    "sec-ch-ua-mobile",
    "sec-ch-ua-platform",
    "sec-fetch-dest",
    "sec-fetch-mode",
    "sec-fetch-site",
  ]);

  page.on("request", (req) => {
    const url = req.url();
    if (captured || !url.includes("UserHomefeedResource")) {
      return;
    }

    try {
      const parsed = new URL(url);
      const data = parsed.searchParams.get("data");
      if (!data) {
        return;
      }
      const decodedRaw = decodeURIComponent(data);
      const decoded = JSON.parse(decodedRaw);
      const found = decoded?.options?.bookmarks?.[0];
      if (found) {
        bookmark = found;
        captured = true;
        capturedDataJSON = decodedRaw;
        capturedSourceURL = parsed.searchParams.get("source_url") || "/";
        const headers = req.headers();
        capturedHeaders = {};
        for (const [key, value] of Object.entries(headers)) {
          const lower = key.toLowerCase();
          if (headerAllowlist.has(lower)) {
            capturedHeaders[lower] = value;
          }
        }
        capturedUserAgent = headers["user-agent"] || "";
        console.log("Captured bookmark.");
      }
    } catch (err) {
      console.error("Failed to parse homefeed request:", err);
    }
  });

  await page.goto("https://www.pinterest.com/login");
  console.log("Log in manually in the opened browser...");
  await page.waitForURL("https://www.pinterest.com/");
  await page.waitForTimeout(5000);

  const cookies = await context.cookies();
  const pinterestCookies = cookies.filter(isPinterestCookie);
  const output = {
    cookies: pinterestCookies,
    cookies_header: cookiesToHeader(pinterestCookies),
    headers: capturedHeaders,
    user_agent: capturedUserAgent,
    data_json: capturedDataJSON,
    source_url: capturedSourceURL,
    bookmark,
    captured_at: new Date().toISOString(),
  };

  const json = JSON.stringify(output, null, 2);
  if (process.env.CAPTURE_OUTPUT_FILE) {
    const fs = require("fs");
    fs.writeFileSync(process.env.CAPTURE_OUTPUT_FILE, json);
  }

  console.log(json);
  await browser.close();
})();
