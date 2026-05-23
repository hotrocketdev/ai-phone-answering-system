import { Controller, Post, Res, HttpCode } from '@nestjs/common';
import type { FastifyReply } from 'fastify';

@Controller('api/public/voice')
export class VoiceController {
  @Post('webhook')
  @HttpCode(200)
  async handleIncomingCall(@Res() res: FastifyReply): Promise<void> {
    const wsUrl = process.env.GATEWAY_WS_URL || 'wss://voice.voxlane.com/stream';

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

  @Post('status-callback')
  @HttpCode(204)
  async handleStatusCallback(): Promise<void> {
    // Twilio sends call status updates here (completed, busy, no-answer, etc.)
    // For PoC, just acknowledge
  }
}
