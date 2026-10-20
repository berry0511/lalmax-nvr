/** Honor the caller's media URL for independent streams as well as cameras. */
export function selectFlvStreamURL(streamUrl: string, fallbackUrl: string): string {
  return streamUrl || fallbackUrl;
}
