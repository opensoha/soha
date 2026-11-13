import assert from "node:assert/strict";
import { test } from "node:test";

import {
  calculateFeishuSignature,
  encryptFeishuWebhookPayload,
  parseFeishuWebhook
} from "../src/connectors/feishu/webhook.js";

const verificationToken = "test-verification-token";
const encryptKey = "test-encrypt-key";

test("Feishu URL verification can be decrypted without signature headers", () => {
  const challengePayload = {
    type: "url_verification",
    token: verificationToken,
    challenge: "challenge-value"
  };
  const encrypted = encryptFeishuWebhookPayload(challengePayload, encryptKey, new Uint8Array(16).fill(7));

  const payload = parseFeishuWebhook(
    {
      headers: {},
      rawBody: JSON.stringify({
        encrypt: encrypted
      })
    },
    {
      verificationToken,
      encryptKey
    }
  );

  assert.equal(payload.type, "url_verification");
  assert.equal(payload.challenge, "challenge-value");
});

test("Feishu event requires a valid signature when encryptKey is configured", () => {
  const body = JSON.stringify(messagePayload());

  assert.throws(
    () =>
      parseFeishuWebhook(
        {
          headers: {},
          rawBody: body
        },
        {
          verificationToken,
          encryptKey
        }
      ),
    /signature headers are incomplete/
  );

  assert.throws(
    () =>
      parseFeishuWebhook(
        {
          headers: {
            "X-Lark-Request-Timestamp": "1710000000",
            "X-Lark-Request-Nonce": "nonce",
            "X-Lark-Signature": "00".repeat(32)
          },
          rawBody: body
        },
        {
          verificationToken,
          encryptKey
        }
      ),
    /signature mismatch/
  );
});

test("Feishu signature verification preserves raw body semantics", () => {
  const body = JSON.stringify(messagePayload());
  const signature = calculateFeishuSignature({
    timestamp: "1710000000",
    nonce: "nonce",
    encryptKey,
    rawBody: body
  });

  const payload = parseFeishuWebhook(
    {
      headers: {
        "X-Lark-Request-Timestamp": "1710000000",
        "X-Lark-Request-Nonce": "nonce",
        "X-Lark-Signature": signature
      },
      rawBody: body
    },
    {
      verificationToken,
      encryptKey
    }
  );

  assert.equal(payload.header?.event_id, "event-1");
  assert.equal(payload.header?.event_type, "im.message.receive_v1");
});

test("Feishu webhook rejects token mismatch", () => {
  assert.throws(
    () =>
      parseFeishuWebhook(
        {
          headers: {},
          rawBody: JSON.stringify({
            type: "url_verification",
            token: "wrong-token",
            challenge: "challenge-value"
          })
        },
        {
          verificationToken
        }
      ),
    /token mismatch/
  );
});

function messagePayload() {
  return {
    schema: "2.0",
    header: {
      event_id: "event-1",
      event_type: "im.message.receive_v1",
      create_time: "1710000000000",
      token: verificationToken,
      tenant_key: "tenant-1"
    },
    event: {
      sender: {
        sender_id: {
          open_id: "ou_sender"
        }
      },
      message: {
        message_id: "om_message",
        chat_id: "oc_chat",
        message_type: "text",
        content: "{\"text\":\"hello\"}"
      }
    }
  };
}
