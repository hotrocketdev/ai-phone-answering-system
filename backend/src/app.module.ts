import { Module } from '@nestjs/common';
import { ToolsModule } from './modules/tools/tools.module';

@Module({
  imports: [ToolsModule],
})
export class AppModule {}
