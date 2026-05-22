// === Booking Types ===

export interface BookingData {
  partySize: number;
  date: string;
  time: string;
  name: string;
  phone: string;
  email?: string;
  notes?: string;
  reference?: string;
}

export interface BookingResult {
  success: boolean;
  bookingRef?: string;
  error?: string;
}
