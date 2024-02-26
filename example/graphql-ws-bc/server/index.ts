// The example is copied from ws server usage with subscriptions-transport-ws backwards compatibility example
// https://the-guild.dev/graphql/ws/recipes#ws-server-usage-with-subscriptions-transport-ws-backwards-compatibility

import http from "http";
import { WebSocketServer } from "ws"; // yarn add ws
// import ws from 'ws'; yarn add ws@7
// const WebSocketServer = ws.Server;
import { execute, subscribe } from "graphql";
import { GRAPHQL_TRANSPORT_WS_PROTOCOL } from "graphql-ws";
import { useServer } from "graphql-ws/lib/use/ws";
import { SubscriptionServer, GRAPHQL_WS } from "subscriptions-transport-ws";
import { schema } from "./schema";

// extra in the context
interface Extra {
  readonly request: http.IncomingMessage;
}

// your custom auth
class Forbidden extends Error {}
function handleAuth(request: http.IncomingMessage) {
  // do your auth on every subscription connect
  const token = request.headers["authorization"];

  // or const { iDontApprove } = session(request.cookies);
  if (token !== "Bearer random-secret") {
    // throw a custom error to be handled
    throw new Forbidden(":(");
  }
}

// graphql-ws
const graphqlWs = new WebSocketServer({ noServer: true });
useServer(
  {
    schema,
    onConnect: async (ctx) => {
      // do your auth on every connect (recommended)
      await handleAuth(ctx.extra.request);
    },
  },
  graphqlWs
);

// subscriptions-transport-ws
const subTransWs = new WebSocketServer({ noServer: true });
SubscriptionServer.create(
  {
    schema,
    execute,
    subscribe,
  },
  subTransWs
);

// create http server
const server = http.createServer(function weServeSocketsOnly(_, res) {
  res.writeHead(404);
  res.end();
});

// listen for upgrades and delegate requests according to the WS subprotocol
server.on("upgrade", (req, socket, head) => {
  // extract websocket subprotocol from header
  const protocol = req.headers["sec-websocket-protocol"];
  const protocols = Array.isArray(protocol)
    ? protocol
    : protocol?.split(",").map((p) => p.trim());

  // decide which websocket server to use
  const wss =
    protocols?.includes(GRAPHQL_WS) && // subscriptions-transport-ws subprotocol
    !protocols.includes(GRAPHQL_TRANSPORT_WS_PROTOCOL) // graphql-ws subprotocol
      ? subTransWs
      : // graphql-ws will welcome its own subprotocol and
        // gracefully reject invalid ones. if the client supports
        // both transports, graphql-ws will prevail
        graphqlWs;
  wss.handleUpgrade(req, socket, head, (ws) => {
    wss.emit("connection", ws, req);
  });
});

const port = 4000;
console.log(`listen server on localhost:${port}`);
server.listen(port);
