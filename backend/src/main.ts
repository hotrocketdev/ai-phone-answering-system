import { NestFactory } from '@nestjs/core';
import { FastifyAdapter, NestFastifyApplication } from '@nestjs/platform-fastify';
import { AppModule } from './app.module';

async function bootstrap() {
  const app = await NestFactory.create<NestFastifyApplication>(
    AppModule,
    new FastifyAdapter(),
  );

  const port = parseInt(process.env.NESTJS_PORT || '3000', 10);

  // Internal API
  await app.listen(port, '0.0.0.0');
  console.log(`VoxLane Backend listening on :${port}`);
}

bootstrap();
