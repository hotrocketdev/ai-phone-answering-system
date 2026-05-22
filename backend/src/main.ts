import { NestFactory } from '@nestjs/core';
import { FastifyAdapter, NestFastifyApplication } from '@nestjs/platform-fastify';
import { AppModule } from './app.module';

async function bootstrap() {
  const app = await NestFactory.create<NestFastifyApplication>(
    AppModule,
    new FastifyAdapter(),
  );

  // Internal API on port 3000
  await app.listen(3000, '0.0.0.0');
  console.log('VoxLane Backend listening on :3000');
}

bootstrap();
