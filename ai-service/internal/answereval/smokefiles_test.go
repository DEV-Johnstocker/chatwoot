package answereval

import (
	"encoding/json"
	"os"
	"testing"
)

// The smoke case files ship in the image (/testdata); a broken one would only
// show up on staging or in the nightly run. Issue: DEV-Johnstocker/chatwoot#1
func TestSmokeCaseFilesAreValid(t *testing.T) {
	for _, path := range []string{"../../testdata/answers-smoke.json", "../../testdata/answers-smoke-studio.json"} {
		// Arrange
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var cases []Case

		// Act
		err = json.Unmarshal(raw, &cases)
		if err == nil {
			err = Validate(cases)
		}

		// Assert
		if err != nil || len(cases) != 15 {
			t.Errorf("%s: %d cases, err %v; want 15 valid cases", path, len(cases), err)
		}
	}
}
