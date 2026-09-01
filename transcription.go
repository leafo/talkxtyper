package main

import (
	"context"
	"fmt"
)

type liveTranscriptionSessionAPI interface {
	AppendPCM(context.Context, []int16) error
	CommitAndWait(context.Context) (string, error)
	liveText() string
	liveTextReadyCh() <-chan struct{}
	Close()
}

// liveFinalizationReporter is implemented by live sessions that can explain
// how their last CommitAndWait ended, for the history view.
type liveFinalizationReporter interface {
	finalizationReason() string
}

func transcribeAudio(ctx context.Context, provider TranscriptionProvider, mp3FilePath string, transcriptionContext TranscriptionContext) (*TranscriptionResult, error) {
	switch normalizeTranscriptionProvider(provider) {
	case TranscriptionProviderGemini:
		return transcribeGeminiAudio(ctx, mp3FilePath, transcriptionContext)
	case TranscriptionProviderOpenAI:
		return transcribeOpenAIAudio(ctx, mp3FilePath, transcriptionContext)
	default:
		return nil, fmt.Errorf("unsupported transcription provider %q", provider)
	}
}

// newConfiguredLiveTranscriptionSession takes the transcription context as a
// channel so each provider waits for it as late as it can: Gemini needs it to
// open the connection, while OpenAI only needs it to configure an already
// connected session, letting its handshake overlap with context collection.
func newConfiguredLiveTranscriptionSession(ctx context.Context, provider TranscriptionProvider, contextCh <-chan TranscriptionContext) (liveTranscriptionSessionAPI, string, TranscriptionContext, error) {
	switch normalizeTranscriptionProvider(provider) {
	case TranscriptionProviderGemini:
		transcriptionContext := <-contextCh
		session, err := newGeminiLiveTranscriptionSession(ctx, transcriptionContext)
		if err != nil {
			return nil, "", transcriptionContext, err
		}
		return session, geminiModelLabel(geminiLiveTranscriptionModel), transcriptionContext, nil
	case TranscriptionProviderOpenAI:
		apiKey, err := getOpenAIAPIKey()
		if err != nil {
			return nil, "", TranscriptionContext{}, err
		}
		session, err := newOpenAILiveTranscriptionSession(ctx, apiKey, openAIClientSecretURL, openAIRealtimeWebSocketURL)
		if err != nil {
			return nil, "", TranscriptionContext{}, err
		}
		transcriptionContext := <-contextCh
		if err := session.Configure(ctx, transcriptionContext); err != nil {
			session.Close()
			return nil, "", transcriptionContext, fmt.Errorf("configuring live transcription: %w", err)
		}
		return session, openAILiveTranscriptionModel, transcriptionContext, nil
	default:
		return nil, "", TranscriptionContext{}, fmt.Errorf("unsupported transcription provider %q", provider)
	}
}

func liveSampleRate(provider TranscriptionProvider) int {
	if normalizeTranscriptionProvider(provider) == TranscriptionProviderGemini {
		return geminiLiveSampleRate
	}
	return openAILiveSampleRate
}
