import { Controller, Post, Body, UseGuards } from '@nestjs/common';
import { HmacGuard } from '../../common/guards/hmac.guard';

interface ToolCallBody {
  callSid: string;
  tenantId: string;
  toolName: string;
  arguments: Record<string, unknown>;
}

@Controller('api/internal/tools')
export class ToolsController {
  @Post('check-availability')
  @UseGuards(HmacGuard)
  async checkAvailability(@Body() body: ToolCallBody) {
    const args = body.arguments as { date?: string; time?: string; partySize?: number };
    console.log(`[${body.callSid}] check_availability: date=${args.date}, time=${args.time}, partySize=${args.partySize}`);

    // Fake: always return available with slots
    return {
      success: true,
      data: {
        available: true,
        slots: ['19:00', '19:15', '19:30', '19:45', '20:00', '20:15'],
      },
    };
  }

  @Post('create-booking')
  @UseGuards(HmacGuard)
  async createBooking(@Body() body: ToolCallBody) {
    const args = body.arguments as { date?: string; time?: string; partySize?: number; name?: string; phone?: string };
    console.log(`[${body.callSid}] create_booking: name=${args.name}, date=${args.date}, time=${args.time}, party=${args.partySize}`);

    // Fake: always succeed with a reference
    const bookingRef = `BK-${Date.now().toString(36).toUpperCase()}`;
    return {
      success: true,
      data: {
        bookingRef,
        date: args.date,
        time: args.time,
        partySize: args.partySize,
        name: args.name,
      },
    };
  }

  @Post('cancel-booking')
  @UseGuards(HmacGuard)
  async cancelBooking(@Body() body: ToolCallBody) {
    console.log(`[${body.callSid}] cancel_booking`);
    return { success: true, data: { message: 'Booking cancelled' } };
  }

  @Post('modify-booking')
  @UseGuards(HmacGuard)
  async modifyBooking(@Body() body: ToolCallBody) {
    console.log(`[${body.callSid}] modify_booking`);
    return { success: true, data: { message: 'Booking modified' } };
  }

  @Post('lookup-reservation')
  @UseGuards(HmacGuard)
  async lookupReservation(@Body() body: ToolCallBody) {
    console.log(`[${body.callSid}] lookup_reservation`);
    return {
      success: true,
      data: {
        found: true,
        reservation: {
          reference: 'BK-EXISTING',
          date: '2026-05-22',
          time: '19:00',
          partySize: 4,
          name: 'Existing Booking',
        },
      },
    };
  }

  @Post('transfer-call')
  @UseGuards(HmacGuard)
  async transferCall(@Body() body: ToolCallBody) {
    console.log(`[${body.callSid}] transfer_call`);
    return { success: true, data: { transferred: true } };
  }
}
