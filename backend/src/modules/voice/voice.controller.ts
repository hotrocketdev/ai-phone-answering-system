import { Controller, Post, Res, Req, HttpCode } from '@nestjs/common';
import type { FastifyReply, FastifyRequest } from 'fastify';

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
    const wsUrl = process.env.GATEWAY_WS_URL || 'wss://voice.voxlane.com/stream';
    const callSid = (req.body as any)?.CallSid || 'unknown';
    console.log(`[${callSid}] Twilio voice webhook`);

    const twiml = `<?xml version="1.0" encoding="UTF-8"?>
<Response>
  <Connect>
    <Stream url="${wsUrl}">
      <Parameter name="callerId" value="{{From}}"/>
    </Stream>
  </Connect>
  <Say>Sorry, we're experiencing technical difficulties. Please call back shortly.</Say>
</Response>`;

    res.header('Content-Type', 'text/xml');
    res.send(twiml);
  }

  /**
   * Telnyx voice webhook — returns JSON call control.
   * STATUS: Scaffold — not yet tested with real Telnyx account.
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
    console.log(`[${callControlId}] Telnyx voice webhook (scaffold)`);

    // Telnyx expects JSON, not XML
    res.header('Content-Type', 'application/json');
    res.send({
      stream_url: wsUrl,
      stream_track: 'both_tracks',
      client_state: callControlId,
    });
  }

  /**
   * SignalWire voice webhook — placeholder.
   */
  @Post('webhook/signalwire')
  @HttpCode(501)
  async handleSignalWireWebhook(@Res() res: FastifyReply): Promise<void> {
    res.send({ error: 'SignalWire not yet implemented' });
  }

  @Post('status-callback')
  @HttpCode(204)
  async handleStatusCallback(): Promise<void> {}
}
