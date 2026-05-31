import { Controller, Post, Res, Req, HttpCode } from '@nestjs/common';
import type { FastifyReply, FastifyRequest } from 'fastify';
import * as https from 'https';

function xmlEscape(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

// callTelnyx sends a REST API command and logs the result.
function callTelnyx(callControlId: string, action: string, body: any, apiKey: string): void {
  const postData = JSON.stringify(body);
  const opts = {
    hostname: 'api.telnyx.com',
    path: `/v2/calls/${callControlId}/actions/${action}`,
    method: 'POST',
    headers: {
      'Authorization': `Bearer ${apiKey}`,
      'Content-Type': 'application/json',
      'Content-Length': Buffer.byteLength(postData),
    },
  };
  const req = https.request(opts, (res) => {
    let d = '';
    res.on('data', (c: string) => d += c);
    res.on('end', () => console.log(`[${callControlId}] Telnyx ${action}: HTTP ${res.statusCode} ${d.substring(0, 200)}`));
  });
  req.on('error', (e: Error) => console.error(`[${callControlId}] Telnyx ${action} error: ${e.message}`));
  req.write(postData);
  req.end();
}

@Controller('api/public/voice')
export class VoiceController {
  @Post('webhook')
  @HttpCode(200)
  async handleTwilioWebhook(
    @Req() req: FastifyRequest,
    @Res() res: FastifyReply,
  ): Promise<void> {
    const body: Record<string, string> = (req.body as any) || {};
    const callSid = body.CallSid || 'unknown';
    const from = body.From || 'unknown';
    console.log(`[${callSid}] Twilio voice webhook — from=${from}`);
    const wsUrl = process.env.GATEWAY_WS_URL || 'wss://voice.voxlane.com/stream';
    const streamUrl = `${wsUrl}/${xmlEscape(callSid)}`;
    const twiml = `<?xml version="1.0" encoding="UTF-8"?>
<Response>
  <Connect>
    <Stream url="${streamUrl}">
      <Parameter name="callerId" value="${xmlEscape(from)}"/>
    </Stream>
  </Connect>
  <Say>Sorry, we're experiencing technical difficulties. Please call back shortly.</Say>
</Response>`;
    res.header('Content-Type', 'text/xml');
    res.send(twiml);
  }

  @Post('webhook/telnyx')
  @HttpCode(200)
  async handleTelnyxWebhook(
    @Req() req: FastifyRequest,
    @Res() res: FastifyReply,
  ): Promise<void> {
    const wsUrl = process.env.GATEWAY_WS_URL || '';
    const body = req.body as any;
    const callControlId = body?.data?.payload?.call_control_id ||
                          body?.data?.call_control_id ||
                          '';
    if (!callControlId) {
      res.send({ status: 'error', detail: 'missing call_control_id' });
      return;
    }
    const apiKey = process.env.TELNYX_API_KEY || '';
    const eventType = body?.data?.event_type || 'unknown';
    console.log(`[${callControlId}] Telnyx ${eventType}`);

    // Respond 200 immediately
    res.status(200).send({ status: 'ok' });

    if (eventType !== 'call.initiated' || !apiKey) return;

    const streamUrl = `${wsUrl}/${callControlId}`;

    // 1. Answer the call
    callTelnyx(callControlId, 'answer', {}, apiKey);

    // 2. Start streaming after a short delay for answer to process
    setTimeout(() => {
      callTelnyx(callControlId, 'streaming_start', {
        stream_url: streamUrl,
        stream_track: 'both_tracks',
        stream_bidirectional_codec: 'PCMU',
      }, apiKey);
    }, 2000);
  }

  @Post('webhook/signalwire')
  @HttpCode(501)
  async handleSignalWireWebhook(@Res() res: FastifyReply): Promise<void> {
    res.send({ error: 'SignalWire not yet implemented' });
  }

  @Post('status-callback')
  @HttpCode(204)
  async handleStatusCallback(): Promise<void> {}
}
