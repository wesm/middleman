import { stopE2EServer } from "./e2eServer";

export default async function globalTeardown(): Promise<void> {
  await stopE2EServer();
}
