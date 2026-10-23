// Cloudflare Email Worker deployed by mailer. Email Routing hands every
// message for the domain to this Worker, which posts it through the
// Cloudflare Tunnel to the mailer server at home. Nothing is stored here.
//
// Bindings (set by mailer when it deploys this script):
//   INBOUND_URL     https://<inbound host>/inbound
//   INBOUND_SECRET  shared secret proving the request came from this Worker

export default {
  async email(message, env, ctx) {
    const raw = await new Response(message.raw).arrayBuffer();

    let lastError = "";
    for (let attempt = 0; attempt < 3; attempt++) {
      if (attempt > 0) await new Promise((r) => setTimeout(r, 2000 * attempt));
      let resp;
      try {
        resp = await fetch(env.INBOUND_URL, {
          method: "POST",
          headers: {
            "Authorization": "Bearer " + env.INBOUND_SECRET,
            "X-Mail-From": message.from,
            "X-Rcpt-To": message.to,
            "Content-Type": "message/rfc822",
          },
          body: raw,
        });
      } catch (e) {
        lastError = String(e);
        continue;
      }
      if (resp.ok) return;
      if (resp.status === 550 || resp.status === 552) {
        message.setReject(await resp.text());
        return;
      }
      lastError = "HTTP " + resp.status;
    }
    // Mail server unreachable: fail so Cloudflare reports an error to the
    // sender rather than silently dropping the message.
    throw new Error("mailer unreachable: " + lastError);
  },
};
