package provider

func ZenProfile() Profile        { return defaultProfile("zen") }
func NvidiaProfile() Profile     { return defaultProfile("nvidia") }
func GeminiProfile() Profile     { return defaultProfile("gemini") }
func OpenRouterProfile() Profile { return defaultProfile("openrouter") }
