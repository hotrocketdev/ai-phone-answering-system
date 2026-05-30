import { Controller, Post, Res, Req, HttpCode } from '@nestjs/common';
import type { FastifyReply, FastifyRequest } from 'fastify';

function xmlEscape(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
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
    console.log(`[${callControlId}] Telnyx webhook`);

    const streamUrl = `${wsUrl}/${callControlId}`;
    res.header('Content-Type', 'application/json');
    res.send({
      data: [
        { call_control_id: callControlId, command: 'answer' },
        {
          call_control_id: callControlId,
          command: 'stream.start',
          stream_url: streamUrl,
          stream_track: 'both_tracks',
        },
      ],
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
