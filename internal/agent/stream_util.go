package agent

const streamChunkRuneSize = 24

// EmitStreamChunks sends text in small chunks through onChunk (e.g. cache hits or agent mode).
func EmitStreamChunks(onChunk func(string) error, text string) error {
	if onChunk == nil || text == "" {
		return nil
	}

	runes := []rune(text)
	for i := 0; i < len(runes); i += streamChunkRuneSize {
		end := i + streamChunkRuneSize
		if end > len(runes) {
			end = len(runes)
		}
		if err := onChunk(string(runes[i:end])); err != nil {
			return err
		}
	}
	return nil
}
