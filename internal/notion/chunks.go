package notion

const blocksPerRequest = 100

func ChunkBlocks(blocks []map[string]any) [][]map[string]any {
	if len(blocks) == 0 {
		return nil
	}
	out := make([][]map[string]any, 0, (len(blocks)+blocksPerRequest-1)/blocksPerRequest)
	for i := 0; i < len(blocks); i += blocksPerRequest {
		end := i + blocksPerRequest
		if end > len(blocks) {
			end = len(blocks)
		}
		out = append(out, blocks[i:end])
	}
	return out
}
