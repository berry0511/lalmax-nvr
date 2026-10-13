import type { Detection, ObjectDetectorOptions } from './inference';

type Reply = { id: number; type: string; detections?: Detection[]; error?: string };
type Pending = { resolve: (value: Reply) => void; reject: (error: Error) => void };

let worker: Worker | null = null;
let nextID = 0;
const pending = new Map<number, Pending>();

function getWorker(): Worker {
  if (worker) return worker;
  worker = new Worker(new URL('./inference-worker.ts', import.meta.url), { type: 'module' });
  worker.onmessage = (event: MessageEvent<Reply>) => {
    const reply = event.data;
    const wait = pending.get(reply.id);
    if (!wait) return;
    pending.delete(reply.id);
    if (reply.type === 'error') wait.reject(new Error(reply.error || 'Browser inference failed'));
    else wait.resolve(reply);
  };
  worker.onerror = (event) => {
    const error = new Error(event.message || 'Browser inference worker failed');
    for (const wait of pending.values()) wait.reject(error);
    pending.clear();
    worker?.terminate();
    worker = null;
  };
  return worker;
}

function request(message: Record<string, unknown>, transfer: Transferable[] = []): Promise<Reply> {
  const id = ++nextID;
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject });
    getWorker().postMessage({ ...message, id }, transfer);
  });
}

export async function registerBrowserDetector(
  cameraId: string,
  modelUrl: string,
  timeoutMs: number,
  options: ObjectDetectorOptions,
): Promise<void> {
  await request({ type: 'init', modelUrl, timeoutMs });
  await request({ type: 'register', cameraId, options });
}

export async function detectBrowserFrame(cameraId: string, frame: VideoFrame): Promise<Detection[]> {
  try {
    const reply = await request({ type: 'detect', cameraId, frame }, [frame]);
    return reply.detections ?? [];
  } catch (error) {
    frame.close();
    throw error;
  }
}

export function disposeBrowserDetector(cameraId: string): void {
  if (worker) void request({ type: 'dispose', cameraId }).catch(() => {});
}
