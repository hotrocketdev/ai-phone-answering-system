import { Controller, Post, Res, Req, HttpCode } from '@nestjs/common';
import type { FastifyReply, FastifyRequest } from 'fastify';

function xmlEscape(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

@Controller('api/public/voice')
export class VoiceController {
  /**
   * Twilio voice webhook — returns TwiML with <Connect><Stream>.
   * Twilio sends application/x-www-form-urlencoded with CallSid, From, To, etc.
   */
  @Post('webhook')
  @HttpCode(200)
  async handleTwilioWebhook(
    @Req() req: FastifyRequest,
    @Res() res: FastifyReply,
  ): Promise<void> {
    // Twilio sends application/x-www-form-urlencoded
    // Fastify may or may not parse this — handle both cases
    const body: Record<string, string> = (req.body as any) || {};
    const callSid = body.CallSid || 'unknown';
    const from = body.From || 'unknown';
    const to = body.To || 'unknown';

    console.log(`[${callSid}] Twilio voice webhook — from=${from} to=${to}`);

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

    console.log(`[${callSid}] TwiML stream URL: ${streamUrl}`);

    res.header('Content-Type', 'text/xml');
    res.send(twiml);
  }

  /**
   * Telnyx voice webhook — returns JSON call control.
   */
  @Post('webhook/telnyx')
  @HttpCode(200)
  async handleTelnyxWebhook(
    @Req() req: FastifyRequest,
    @Res() res: FastifyReply,
  ): Promise<void> {
    const wsUrl = process.env.GATEWAY_WS_URL || '';
    const body = req.body as any;
    const callControlId = body?.data?.call_control_id || 'unknown';
    console.log(`[${callControlId}] Telnyx voice webhook`);

    const streamUrl = `${wsUrl}/${callControlId}`;

    res.header('Content-Type', 'application/json');
    res.send({
      stream_url: streamUrl,
      stream_track: 'both_tracks',
      client_state: callControlId,
    });
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
