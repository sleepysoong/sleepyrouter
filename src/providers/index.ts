import { registerProvider } from "./base.js";
import { openRouterProvider } from "./openrouter.js";
import { nvidiaProvider } from "./nvidia.js";
import { copilotProvider } from "./copilot.js";
import { googleProvider } from "./google.js";
import { zenProvider } from "./zen.js";

export * from "./base.js";
export * from "./openrouter.js";
export * from "./nvidia.js";
export * from "./copilot.js";
export * from "./google.js";
export * from "./zen.js";

// Register default providers
registerProvider("openrouter", openRouterProvider);
registerProvider("nvidia", nvidiaProvider);
registerProvider("copilot", copilotProvider);
registerProvider("google", googleProvider);
registerProvider("zen", zenProvider);
