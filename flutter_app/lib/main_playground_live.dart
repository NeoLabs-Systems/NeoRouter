import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:google_fonts/google_fonts.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/models.dart';

/// The playground's live voice mode. A live call is a real-time spoken
/// conversation over a WebSocket, so instead of a prompt box this shows where
/// apps connect for each protocol and which live models answer there.
class LivePlayground extends StatefulWidget {
  const LivePlayground({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<LivePlayground> createState() => _LivePlaygroundState();
}

const Map<String, String> _protocolNames = <String, String>{
  'openai_live': 'OpenAI GPT-Live',
  'openai_realtime': 'OpenAI Realtime',
  'gemini_live': 'Gemini Live',
};

class _LivePlaygroundState extends State<LivePlayground> {
  List<CatalogModel> _models = <CatalogModel>[];
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final models = await widget.controller.api.models(capability: 'live');
      if (mounted) setState(() => _models = models.where((model) => model.enabled).toList());
    } catch (error) {
      if (mounted) setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  // The gateway's address per protocol, from the /v1 endpoint.
  Map<String, String> _addresses() {
    final api = (widget.controller.endpoint ?? '${Uri.base.origin}/v1').replaceFirst(RegExp(r'^http'), 'ws');
    final origin = api.replaceFirst(RegExp(r'/v1/?$'), '');
    return <String, String>{
      'openai_live': '$api/live/sessions',
      'openai_realtime': '$api/realtime?model=auto',
      'gemini_live': '$origin/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent',
    };
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final addresses = _addresses();
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 780),
        child: ListView(
          padding: const EdgeInsets.only(bottom: 24),
          children: <Widget>[
            Text(t.pgLiveHint, style: TextStyle(color: palette.textSoft, height: 1.45)),
            const SizedBox(height: AppSpacing.lg),
            SettingsHeading(title: t.pgLiveEndpoints, description: t.pgLiveModelHint),
            SettingsCardList(
              children: <Widget>[
                for (final entry in addresses.entries) _AddressRow(label: _protocolNames[entry.key]!, address: entry.value),
              ],
            ),
            const SizedBox(height: AppSpacing.lg),
            SettingsHeading(title: t.pgLiveModels),
            AsyncBody(
              loading: _loading,
              error: _error,
              onRetry: _load,
              child: _models.isEmpty
                  ? AppCard(child: Text(t.pgLiveNoModels, style: TextStyle(color: palette.textSoft)))
                  : SettingsCardList(children: <Widget>[for (final model in _models) _ModelRow(model: model)]),
            ),
          ],
        ),
      ),
    );
  }
}

class _AddressRow extends StatelessWidget {
  const _AddressRow({required this.label, required this.address});

  final String label;
  final String address;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return Padding(
      padding: const EdgeInsets.fromLTRB(20, 12, 8, 12),
      child: Row(
        children: <Widget>[
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(label, style: TextStyle(fontWeight: FontWeight.w600, color: palette.text)),
                const SizedBox(height: 3),
                SelectableText(address, style: GoogleFonts.geistMono(fontSize: 12, color: palette.textSoft)),
              ],
            ),
          ),
          IconButton(
            tooltip: t.copy,
            icon: const Icon(Icons.copy_rounded, size: 17),
            onPressed: () {
              Clipboard.setData(ClipboardData(text: address));
              showSnack(context, t.copied);
            },
          ),
        ],
      ),
    );
  }
}

class _ModelRow extends StatelessWidget {
  const _ModelRow({required this.model});

  final CatalogModel model;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final protocols = model.liveProtocols;
    return Padding(
      padding: const EdgeInsets.fromLTRB(20, 12, 20, 12),
      child: Row(
        children: <Widget>[
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(model.name, style: TextStyle(fontWeight: FontWeight.w600, color: palette.text)),
                const SizedBox(height: 3),
                Text(
                  <String>[model.providerSlug, model.isFree ? t.free : t.paid].join(' · '),
                  style: TextStyle(fontSize: 12.5, color: palette.textMuted),
                ),
              ],
            ),
          ),
          Wrap(
            spacing: 6,
            children: <Widget>[for (final protocol in protocols) MetaPill(label: _protocolNames[protocol] ?? protocol)],
          ),
        ],
      ),
    );
  }
}
