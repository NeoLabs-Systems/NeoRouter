package adapters

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// The factory: the wiring layer hands it a definition and gets the dialect that
// serves it, or ErrNotChatAdapter for the media adapters another file owns.

// The sign-in service of the parent package satisfies both seams as it stands,
// so internal/app passes it directly.
var (
	_ CodexSignIn = (*providers.OAuth)(nil)
	_ NousSignIn  = (*providers.OAuth)(nil)
)

func TestFactoryBuildsEveryChatDialect(t *testing.T) {
	factory := &Factory{Codex: &fakeCodexSignIn{access: "a"}, Nous: &fakeNousSignIn{}, Local: &fakeLocalModels{}}
	cases := map[string]any{
		"custom_openai":    &openAICompatible{},
		"opencode":         &openAICompatible{},
		"anthropic":        &anthropic{},
		"google":           &googleGemini{},
		"cloudflare":       &cloudflare{},
		"chatgpt":          &codexAdapter{},
		"nous":             &nous{},
		"neorouter_local":  &localRuntime{},
		"custom_anthropic": &anthropic{},
	}
	for id, want := range cases {
		adapter, err := factory.NewAdapter(chatDefinition(t, id), chatDeps{secret: "k", options: map[string]any{"accountId": "a"}}.deps())
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if got, ok := adapter.(interface{ Definition() providers.Definition }); !ok || got.Definition().ID != id {
			t.Errorf("%s: the adapter reports %v", id, adapter)
		}
		switch want.(type) {
		case *openAICompatible:
			if _, ok := adapter.(*openAICompatible); !ok {
				t.Errorf("%s: built %T", id, adapter)
			}
		case *anthropic:
			if _, ok := adapter.(*anthropic); !ok {
				t.Errorf("%s: built %T", id, adapter)
			}
		case *googleGemini:
			if _, ok := adapter.(*googleGemini); !ok {
				t.Errorf("%s: built %T", id, adapter)
			}
		case *cloudflare:
			if _, ok := adapter.(*cloudflare); !ok {
				t.Errorf("%s: built %T", id, adapter)
			}
		case *codexAdapter:
			if _, ok := adapter.(*codexAdapter); !ok {
				t.Errorf("%s: built %T", id, adapter)
			}
		case *nous:
			if _, ok := adapter.(*nous); !ok {
				t.Errorf("%s: built %T", id, adapter)
			}
		case *localRuntime:
			if _, ok := adapter.(*localRuntime); !ok {
				t.Errorf("%s: built %T", id, adapter)
			}
		}
	}
}

func TestFactoryLeavesTheMediaDialectsAlone(t *testing.T) {
	factory := &Factory{}
	for _, name := range []providers.AdapterName{
		providers.AdapterSearch, providers.AdapterWeather, providers.AdapterDeepgram, providers.AdapterElevenLabs,
	} {
		def := providers.Definition{ID: "x", Adapter: name}
		if _, err := factory.NewAdapter(def, chatDeps{}.deps()); !errors.Is(err, ErrNotChatAdapter) {
			t.Errorf("%s: err = %v, want ErrNotChatAdapter", name, err)
		}
	}
	for _, name := range ChatAdapterNames {
		def := providers.Definition{ID: "x", Adapter: name}
		if _, err := factory.NewAdapter(def, chatDeps{}.deps()); errors.Is(err, ErrNotChatAdapter) {
			t.Errorf("%s: the factory must own this dialect", name)
		}
	}
}

func TestFactoryServesTheRegistry(t *testing.T) {
	// The registry builds adapters through the factory it was loaded with, which
	// is how internal/app wires this package.
	factory := &Factory{Codex: &fakeCodexSignIn{access: "a"}, Nous: &fakeNousSignIn{}, Local: &fakeLocalModels{}}
	upstream := openAIFake(t)
	loaded, err := providers.Parse(providersDefinitions(t), factory.AdapterFactory(), nil)
	if err != nil {
		t.Fatalf("load definitions: %v", err)
	}
	def, ok := loaded.Get("custom_openai")
	if !ok {
		t.Fatal("the definition is missing")
	}
	adapter, err := loaded.CreateAdapter(def, chatDeps{baseURL: upstream.url() + "/v1", secret: "k"}.deps())
	if err != nil {
		t.Fatalf("create adapter: %v", err)
	}
	chatter, ok := adapter.(providers.Chatter)
	if !ok {
		t.Fatalf("%T is not a Chatter", adapter)
	}
	call := providers.Call{ModelID: "ok-model"}
	if _, err := chatter.Chat(context.Background(), chatRequest(t, `{"messages":[{"role":"user","content":"hi"}]}`), call); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if got := upstream.last(t).headers.Get("Authorization"); got != "Bearer k" {
		t.Errorf("authorization = %q", got)
	}
}

// providersDefinitions is the definition data the registry the server loads is
// built from.
func providersDefinitions(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../definitions.json")
	if err != nil {
		t.Fatalf("definitions: %v", err)
	}
	return data
}

func TestFactoryChatAdaptersAreStreamers(t *testing.T) {
	factory := &Factory{Codex: &fakeCodexSignIn{access: "a"}, Nous: &fakeNousSignIn{}, Local: &fakeLocalModels{}}
	for _, name := range ChatAdapterNames {
		adapter, err := factory.NewAdapter(providers.Definition{ID: "x", Adapter: name}, chatDeps{}.deps())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, ok := adapter.(providers.Streamer); !ok {
			t.Errorf("%s: every chat dialect streams", name)
		}
		if _, ok := adapter.(providers.Adapter); !ok {
			t.Errorf("%s: the adapter contract is not satisfied", name)
		}
	}
}

func TestFactoryWithoutCollaboratorsStillBuilds(t *testing.T) {
	// A deployment that has not wired the sign-in stores must not panic: the
	// connection reports the failure a person can act on.
	factory := &Factory{}
	adapter, err := factory.NewAdapter(chatDefinition(t, "chatgpt"), chatDeps{secret: `{"tokens":1}`}.deps())
	if err != nil {
		t.Fatalf("create adapter: %v", err)
	}
	_, err = adapter.(providers.Chatter).Chat(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "gpt"})
	if providers.KindOf(err) != providers.KindAuth {
		t.Fatalf("kind = %q: %v", providers.KindOf(err), err)
	}

	nousAdapter, err := factory.NewAdapter(chatDefinition(t, "nous"), chatDeps{secret: "k", baseURL: "http://127.0.0.1:1/v1"}.deps())
	if err != nil {
		t.Fatalf("create adapter: %v", err)
	}
	// Without a Path the client refuses the address; what matters is that it is a
	// classified failure, not a panic.
	_, err = nousAdapter.(providers.Chatter).Chat(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "m"})
	if err == nil {
		t.Error("a call to a dead endpoint must fail")
	}
	if providers.KindOf(err) == "" {
		t.Errorf("the failure must be classified: %v", err)
	}
}
