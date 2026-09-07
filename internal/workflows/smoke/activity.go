package smoke

import "context"

const result = "Temporal activity completed"

func Activity(context.Context) (string, error) {
	return result, nil
}
