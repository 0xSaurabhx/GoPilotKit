package signals

import (
	"testing"
)

func TestExtractPositiveFeedback(t *testing.T) {
	ext := NewExtractor()

	tests := []struct {
		name     string
		feedback string
		wantType SignalType
	}{
		{"great", "This is great!", SignalExplicitPositive},
		{"thanks", "Thanks for the help", SignalExplicitPositive},
		{"helpful", "Very helpful answer", SignalExplicitPositive},
		{"perfect", "Perfect, exactly what I needed", SignalExplicitPositive},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signal := ext.ExtractFromFeedback("run_1", "thread_1", "agent", tt.feedback)
			if signal == nil {
				t.Fatal("expected signal, got nil")
			}
			if signal.Type != tt.wantType {
				t.Errorf("expected type %s, got %s", tt.wantType, signal.Type)
			}
			if signal.Value <= 0 {
				t.Errorf("expected positive value, got %f", signal.Value)
			}
		})
	}
}

func TestExtractNegativeFeedback(t *testing.T) {
	ext := NewExtractor()

	tests := []struct {
		name     string
		feedback string
		wantType SignalType
	}{
		{"wrong", "That's wrong", SignalExplicitNegative},
		{"bad", "Bad answer", SignalExplicitNegative},
		{"bad answer", "This is a bad answer", SignalExplicitNegative},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signal := ext.ExtractFromFeedback("run_1", "thread_1", "agent", tt.feedback)
			if signal == nil {
				t.Fatal("expected signal, got nil")
			}
			if signal.Type != tt.wantType {
				t.Errorf("expected type %s, got %s", tt.wantType, signal.Type)
			}
			if signal.Value >= 0 {
				t.Errorf("expected negative value, got %f", signal.Value)
			}
		})
	}
}

func TestExtractCorrection(t *testing.T) {
	ext := NewExtractor()

	signal := ext.ExtractFromFeedback("run_1", "thread_1", "agent", "Actually, I meant something else")
	if signal == nil {
		t.Fatal("expected signal, got nil")
	}
	if signal.Type != SignalCorrection {
		t.Errorf("expected correction signal, got %s", signal.Type)
	}
}

func TestExtractNeutralFeedback(t *testing.T) {
	ext := NewExtractor()

	signal := ext.ExtractFromFeedback("run_1", "thread_1", "agent", "The weather is nice today")
	if signal != nil {
		t.Error("expected nil signal for neutral feedback")
	}
}

func TestExtractFromInteraction(t *testing.T) {
	ext := NewExtractor()

	// Test copied signal.
	signal := ext.ExtractFromInteraction("run_1", "thread_1", "agent", InteractionAction{
		Type: "response_copied",
	})
	if signal == nil {
		t.Fatal("expected signal, got nil")
	}
	if signal.Type != SignalImplicitPositive {
		t.Errorf("expected implicit positive, got %s", signal.Type)
	}

	// Test edited signal.
	signal = ext.ExtractFromInteraction("run_1", "thread_1", "agent", InteractionAction{
		Type: "message_edited",
	})
	if signal == nil {
		t.Fatal("expected signal, got nil")
	}
	if signal.Type != SignalImplicitNegative {
		t.Errorf("expected implicit negative, got %s", signal.Type)
	}
}

func TestAggregateSignals(t *testing.T) {
	ext := NewExtractor()

	signals := []Signal{
		{Type: SignalExplicitPositive, Value: 0.8, Confidence: 0.9},
		{Type: SignalExplicitPositive, Value: 0.6, Confidence: 0.7},
		{Type: SignalExplicitNegative, Value: -0.5, Confidence: 0.5},
	}

	score := ext.AggregateSignals(signals)
	if score == 0 {
		t.Error("expected non-zero aggregate score")
	}
	// Should be positive overall since we have 2 positive signals.
	if score <= 0 {
		t.Errorf("expected positive aggregate, got %f", score)
	}
}

func TestAggregateEmptySignals(t *testing.T) {
	ext := NewExtractor()

	score := ext.AggregateSignals(nil)
	if score != 0 {
		t.Errorf("expected 0 for empty signals, got %f", score)
	}
}
