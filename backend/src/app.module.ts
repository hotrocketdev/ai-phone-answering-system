import { Module } from '@nestjs/common';
import { ToolsModule } from './modules/tools/tools.module';
import { VoiceModule } from './modules/voice/voice.module';

@Module({
  imports: [ToolsModule, VoiceModule],
})
export class AppModule {}
