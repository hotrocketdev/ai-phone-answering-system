import { Controller, Post, Res, Req, HttpCode } from '@nestjs/common';
import type { FastifyReply, FastifyRequest } from 'fastify';
import * as https from 'https';

function xmlEscape(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

@Controller('api/public/voice')
export class VoiceController {
  /**
   * Twilio voice webhook — returns TwiML with <Connect><Stream>.
   */
  @Post('webhook')
  @HttpCode(200)
  async handleTwilioWebhook(
    @Req() req: FastifyRequest,
    @Res() res: FastifyReply,
  ): Promise<void> {
    const body: Record<string, string> = (req.body as any) || {};
    const callSid = body.CallSid || 'unknown';
    const from = body.From || 'unknown';
    const to = body.To || 'unknown';

    console.log(`[${callSid}] Twilio voice webhook — from=${from} to=${to}`);

    const wsUrl = process.env.GATEWAY_WS_URL || 'wss://voice.voxlane.com/stream';
    const streamUrl = `${wsUrl}/${xmlEscape(callSid)}`;

    console.log(`[${callSid}] TwiML stream URL: ${streamUrl}`);

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

  /**
   * Telnyx voice webhook — responds 200 then sends answer+stream via REST API.
   */
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
                          'unknown';
    const eventType = body?.data?.event_type || 'unknown';
    console.log(`[${callControlId}] Telnyx webhook event=${eventType}`);

    const telnyxKey = process.env.TELNYX_API_KEY || '';
    const streamUrl = `${wsUrl}/${callControlId}`;

    // Acknowledge webhook immediately
    res.header('Content-Type', 'application/json');
    res.status(200).send({ status: 'ok' });

    if (!telnyxKey || callControlId === 'unknown') return;

    // Send answer + stream.start via REST API
    const sendCommand = (commands: any[]) => {
      const postData = JSON.stringify(commands);
      const opts = {
        hostname: 'api.telnyx.com',
        path: `/v2/calls/${callControlId}/actions`,
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${telnyxKey}`,
          'Content-Type': 'application/json',
          'Content-Length': Buffer.byteLength(postData),
        },
      };
      const apiReq = https.request(opts, (apiRes) => {
        let d = '';
        apiRes.on('data', (c: string) => d += c);
        apiRes.on('end', () => console.log(`[${callControlId}] Telnyx API: ${apiRes.statusCode} ${d.substring(0, 200)}`));
      });
      apiReq.on('error', (e: Error) => console.error(`[${callControlId}] Telnyx API error: ${e.message}`));
      apiReq.write(postData);
      apiReq.end();
    };

    // First: answer the call
    sendCommand([{ command: 'answer', client_state: callControlId }]);

    // Then after a short delay: start the media stream
    setTimeout(() => {
      sendCommand([{
        command: 'stream.start',
        stream_url: streamUrl,
        stream_track: 'both_tracks',
        stream_bidirectional_codec: 'PCMU',
        client_state: callControlId,
      }]);
    }, 1000);

    console.log(`[${callControlId}] Telnyx: answer + stream.start queued`);
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
