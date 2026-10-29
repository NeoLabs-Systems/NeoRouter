import 'package:flutter/material.dart';

import '../l10n/gen/app_localizations.dart';

/// Kinds of models NeoRouter knows about. The server reports, per kind, how
/// many models the user's providers offer and whether it can route them yet
/// (`GET /api/v1/models/capabilities`). Screens build their capability
/// switchers from this list, so a new kind only needs an entry here (and
/// strings) once the server starts routing it.
class CapabilityInfo {
  const CapabilityInfo(this.id, this.icon);

  final String id;
  final IconData icon;

  String label(AppL10n t) => switch (id) {
    'chat' => t.capChat,
    'embedding' => t.capEmbedding,
    'rerank' => t.capRerank,
    'image' => t.capImage,
    'audio' => t.capAudio,
    'speech' => t.capSpeech,
    'search' => t.capSearch,
    'transcription' => t.capTranscription,
    'video' => t.capVideo,
    'moderation' => t.capModeration,
    'live' => t.capLive,
    'router' => t.capRouter,
    _ => t.capOther,
  };

  String description(AppL10n t) => switch (id) {
    'chat' => t.capChatHint,
    'embedding' => t.capEmbeddingHint,
    'rerank' => t.capRerankHint,
    'image' => t.capImageHint,
    'audio' => t.capAudioHint,
    'speech' => t.capSpeechHint,
    'search' => t.capSearchHint,
    'transcription' => t.capTranscriptionHint,
    'video' => t.capVideoHint,
    'moderation' => t.capModerationHint,
    'live' => t.capLiveHint,
    'router' => t.capRouterHint,
    _ => t.capOtherHint,
  };
}

const List<CapabilityInfo> kCapabilities = <CapabilityInfo>[
  CapabilityInfo('chat', Icons.chat_bubble_outline_rounded),
  CapabilityInfo('embedding', Icons.scatter_plot_outlined),
  CapabilityInfo('rerank', Icons.sort_rounded),
  CapabilityInfo('image', Icons.image_outlined),
  CapabilityInfo('speech', Icons.record_voice_over_outlined),
  CapabilityInfo('transcription', Icons.subtitles_outlined),
  CapabilityInfo('audio', Icons.graphic_eq_rounded),
  CapabilityInfo('video', Icons.movie_outlined),
  CapabilityInfo('search', Icons.travel_explore_outlined),
  CapabilityInfo('moderation', Icons.bolt_outlined),
  CapabilityInfo('live', Icons.settings_voice_outlined),
  CapabilityInfo('router', Icons.alt_route_outlined),
  CapabilityInfo('other', Icons.category_outlined),
];

CapabilityInfo capabilityInfo(String id) => kCapabilities.firstWhere((item) => item.id == id, orElse: () => kCapabilities.last);

/// One kind as it exists in the user's catalog.
class CapabilitySummary {
  CapabilitySummary(this.raw);
  final Map<String, dynamic> raw;

  String get id => raw['id'] as String;
  int get total => (raw['total'] as num?)?.toInt() ?? 0;
  int get free => (raw['free'] as num?)?.toInt() ?? 0;
  bool get routable => raw['routable'] == true;
  CapabilityInfo get info => capabilityInfo(id);
}

/// Known kinds first, in [kCapabilities] order.
List<CapabilitySummary> sortCapabilities(List<CapabilitySummary> items) {
  int rank(String id) {
    final index = kCapabilities.indexWhere((item) => item.id == id);
    return index < 0 ? kCapabilities.length : index;
  }

  return <CapabilitySummary>[...items]..sort((a, b) => rank(a.id).compareTo(rank(b.id)));
}
