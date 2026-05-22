import { Injectable, CanActivate, ExecutionContext, UnauthorizedException } from '@nestjs/common';
import { createHmac, timingSafeEqual } from 'crypto';

@Injectable()
export class HmacGuard implements CanActivate {
  canActivate(context: ExecutionContext): boolean {
    const request = context.switchToHttp().getRequest();
    const signature = request.headers['x-hmac-signature'];
    const timestamp = parseInt(request.headers['x-timestamp'] as string, 10);

    // Replay protection: 30-second window
    if (isNaN(timestamp) || Math.abs(Date.now() - timestamp) > 30_000) {
      throw new UnauthorizedException('Timestamp expired or missing');
    }

    const body = request.body;
    if (!body || !body.callSid || !body.toolName) {
      throw new UnauthorizedException('Missing required fields');
    }

    const secret = process.env.HMAC_SECRET || 'dev-hmac-secret';
    const payload = `${body.callSid}:${body.tenantId || 'default'}:${body.toolName}:${timestamp}`;
    const expected = createHmac('sha256', secret).update(payload).digest('hex');

    if (!signature || signature.length !== expected.length) {
      throw new UnauthorizedException('Invalid HMAC signature');
    }

    // Constant-time comparison
    const sigBuf = Buffer.from(signature as string, 'hex');
    const expBuf = Buffer.from(expected, 'hex');
    if (sigBuf.length !== expBuf.length || !timingSafeEqual(sigBuf, expBuf)) {
      throw new UnauthorizedException('Invalid HMAC signature');
    }

    return true;
  }
}
