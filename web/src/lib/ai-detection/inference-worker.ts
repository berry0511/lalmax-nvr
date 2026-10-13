/// <reference lib="webworker" />

import { AiRuntime } from './runtime';
import { ObjectDetector, type Detection, type ObjectDetectorOptions } from './inference';

type Request =
  | { id: number; type: 'init'; modelUrl: string; timeoutMs: number }
  | { id: number; type: 'register'; cameraId: string; options: ObjectDetectorOptions }
  | { id: number; type: 'detect'; cameraId: string; frame: VideoFrame }
  | { id: number; type: 'dispose'; cameraId: string };

let runtime: AiRuntime | null = null;
const detectors = new Map<string, ObjectDetector>();
const inFlight = new Set<string>();

const scope = self as unknown as DedicatedWorkerGlobalScope;
scope.onmessage = async (event: MessageEvent<Request>) => {
  const request = event.data;
  try {
    switch (request.type) {
      case 'init': {
        if (!runtime) {
          const next = new AiRuntime();
          await next.init(request.modelUrl, { inferenceTimeoutMs: request.timeoutMs });
          runtime = next;
        }
        reply(request.id, { type: 'ready' });
        break;
      }
      case 'register': {
        if (!runtime) throw new Error('AI runtime is not initialized');
        detectors.get(request.cameraId)?.dispose();
        detectors.set(request.cameraId, new ObjectDetector(runtime, request.options));
        reply(request.id, { type: 'registered' });
        break;
      }
      case 'detect': {
        const detector = detectors.get(request.cameraId);
        if (!detector || inFlight.has(request.cameraId)) {
          request.frame.close();
          reply(request.id, { type: 'detections', detections: [] });
          break;
        }
        inFlight.add(request.cameraId);
        try {
          const detections = await detector.detect(request.frame);
          reply(request.id, { type: 'detections', detections });
        } finally {
          inFlight.delete(request.cameraId);
          request.frame.close();
        }
        break;
      }
      case 'dispose':
        detectors.get(request.cameraId)?.dispose();
        detectors.delete(request.cameraId);
        inFlight.delete(request.cameraId);
        reply(request.id, { type: 'disposed' });
        break;
    }
  } catch (error) {
    reply(request.id, { type: 'error', error: error instanceof Error ? error.message : String(error) });
  }
};

function reply(id: number, result: Record<string, unknown>) {
  scope.postMessage({ id, ...result });
}

export {};
