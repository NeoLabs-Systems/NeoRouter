import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:google_fonts/google_fonts.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';

enum _Tab { privacy, compression }

// A pretty-printed JSON list: compression minifies it and turns it into a table.
const String _compressionSample = '{\n    "orders": [\n        { "id": 1, "status": "shipped", "total": 19.9 },\n        { "id": 2, "status": "shipped", "total": 5.5 },\n'
    '        { "id": 3, "status": "open", "total": 42.0 },\n        { "id": 4, "status": "open", "total": 7.25 }\n    ]\n}';

/// Privacy shield and prompt compression: what happens to a request before
/// it leaves the server.
class ProcessingScreen extends StatefulWidget {
  const ProcessingScreen({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<ProcessingScreen> createState() => _ProcessingScreenState();
}

class _ProcessingScreenState extends State<ProcessingScreen> {
  _Tab _tab = _Tab.privacy;
  Map<String, dynamic>? _settings;
  String _saved = '';
  List<String> _categories = <String>[];
  Map<String, dynamic> _engines = <String, dynamic>{};
  Map<String, dynamic> _stats = <String, dynamic>{};
  bool _loading = true;
  bool _saving = false;
  String? _error;
  late final TextEditingController _terms = TextEditingController();

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _terms.dispose();
    super.dispose();
  }

  Map<String, dynamic> get _privacy => _settings!['privacy'] as Map<String, dynamic>;
  Map<String, dynamic> get _compression => _settings!['compression'] as Map<String, dynamic>;
  bool get _dirty => _settings != null && jsonEncode(_body()) != _saved;

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final api = widget.controller.api;
      final results = await Future.wait(<Future<Map<String, dynamic>>>[api.processingSettings(), api.processingStats()]);
      final data = results[0];
      setState(() {
        _settings = jsonDecode(jsonEncode(data['settings'])) as Map<String, dynamic>;
        _categories = (data['categories'] as List).cast<String>();
        _engines = Map<String, dynamic>.from(data['engines'] as Map);
        _stats = results[1];
        _terms.text = ((_privacy['customTerms'] as List?) ?? const <Object>[]).join('\n');
        _saved = jsonEncode(_body());
      });
    } catch (error) {
      setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Map<String, Object?> _body() {
    final privacy = Map<String, dynamic>.from(_privacy);
    privacy['customTerms'] = _terms.text.split('\n').map((line) => line.trim()).where((line) => line.length >= 2).toList();
    return <String, Object?>{'privacy': privacy, 'compression': _compression};
  }

  Future<void> _save() async {
    final t = AppL10n.of(context);
    setState(() => _saving = true);
    try {
      final body = _body();
      await widget.controller.api.saveProcessingSettings(body);
      setState(() => _saved = jsonEncode(body));
      if (mounted) showSnack(context, t.saved);
    } on ApiException catch (error) {
      if (mounted) showSnack(context, error.message);
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  void _update(VoidCallback change) => setState(change);

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    return PageScaffold(
      header: ScreenHeader(
        title: t.processingTitle,
        description: t.processingDescription,
        trailing: _settings == null || !widget.controller.can('processing.edit')
            ? null
            : FilledButton(
                onPressed: _dirty && !_saving ? _save : null,
                child: _saving ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2)) : Text(t.save),
              ),
      ),
      children: <Widget>[
        PageTabs<_Tab>(
          tabs: <({_Tab value, String label, int? count})>[
            (value: _Tab.privacy, label: t.privacyShield, count: null),
            (value: _Tab.compression, label: t.compressionTitle, count: null),
          ],
          selected: _tab,
          onChanged: (value) => setState(() => _tab = value),
        ),
        AsyncBody(
          loading: _loading,
          error: _error,
          onRetry: _load,
          child: _settings == null ? const SizedBox.shrink() : (_tab == _Tab.privacy ? _privacyView(context) : _compressionView(context)),
        ),
      ],
    );
  }

  // ---- privacy shield ------------------------------------------------------

  Widget _privacyView(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final enabled = _privacy['enabled'] == true;
    final modes = _privacy['modes'] as Map<String, dynamic>;
    final ai = _privacy['ai'] as Map<String, dynamic>;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        MetricStrip(
          metrics: <({String label, String value, String? caption})>[
            (label: t.privacyHiddenValues, value: formatCount((_stats['redacted'] as num?) ?? 0), caption: t.last30Days),
            (label: t.privacyProtectedRequests, value: formatCount((_stats['protectedRequests'] as num?) ?? 0), caption: t.ofRequests(formatCount((_stats['requests'] as num?) ?? 0))),
          ],
        ),
        const SizedBox(height: 28),
        SettingsGroup(
          title: t.privacyShield,
          description: t.privacyHowItWorks,
          children: <Widget>[
            SettingsRow(
              label: t.privacyEnable,
              description: t.privacyEnableHint,
              control: Switch(value: enabled, onChanged: (value) => _update(() => _privacy['enabled'] = value)),
            ),
          ],
        ),
        Opacity(
          opacity: enabled ? 1 : 0.5,
          child: IgnorePointer(
            ignoring: !enabled,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: <Widget>[
                SettingsGroup(
                  title: t.privacyWhatToHide,
                  description: t.privacyModesHint,
                  children: <Widget>[
                    for (final category in _categories)
                      if (category != 'person' && category != 'address' && category != 'organization' && category != 'id')
                        _modeRow(context, category, modes),
                  ],
                ),
                SettingsGroup(
                  title: t.privacyTermsTitle,
                  description: t.privacyTermsHint,
                  children: <Widget>[
                    Padding(
                      padding: const EdgeInsets.all(16),
                      child: TextField(
                        controller: _terms,
                        minLines: 3,
                        maxLines: 8,
                        onChanged: (_) => setState(() {}),
                        decoration: InputDecoration(hintText: t.privacyTermsPlaceholder),
                      ),
                    ),
                  ],
                ),
                SettingsGroup(
                  title: t.privacyAiTitle,
                  description: t.privacyAiHint,
                  children: <Widget>[
                    SettingsRow(
                      label: t.privacyAiEnable,
                      description: t.privacyAiEnableHint,
                      control: Switch(value: ai['enabled'] == true, onChanged: (value) => _update(() => ai['enabled'] = value)),
                    ),
                    if (ai['enabled'] == true) ...<Widget>[
                      for (final category in <String>['person', 'address', 'organization', 'id']) _modeRow(context, category, modes),
                      SettingsRow(
                        label: t.privacyAiModel,
                        description: t.privacyAiModelHint,
                        control: SizedBox(
                          width: 240,
                          child: TextFormField(
                            initialValue: '${ai['model'] ?? 'auto:fast'}',
                            style: GoogleFonts.geistMono(fontSize: 13),
                            onChanged: (value) => _update(() => ai['model'] = value.trim().isEmpty ? 'auto:fast' : value.trim()),
                          ),
                        ),
                      ),
                      SettingsRow(
                        label: t.privacyAiFreeOnly,
                        control: Switch(value: ai['freeOnly'] != false, onChanged: (value) => _update(() => ai['freeOnly'] = value)),
                      ),
                      Padding(
                        padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
                        child: InlineHint(message: t.privacyAiWarning, icon: Icons.info_outline_rounded),
                      ),
                    ],
                  ],
                ),
              ],
            ),
          ),
        ),
        _TryIt(
          key: const ValueKey<String>('privacy'),
          controller: widget.controller,
          // The preview always shows what the shield would do, even while it is off.
          settings: () {
            final body = _body();
            body['privacy'] = <String, dynamic>{...(body['privacy'] as Map<String, dynamic>), 'enabled': true};
            return body;
          },
          hint: t.privacyTryHint,
          sample: t.privacyTrySample,
          describe: (result) {
            final stats = Map<String, dynamic>.from(result['stats'] as Map);
            return t.privacyTryResult('${stats['redacted'] ?? 0}');
          },
        ),
        const SizedBox(height: 8),
        Text(t.privacyFootnote, style: TextStyle(color: palette.textMuted, fontSize: 12.5, height: 1.45)),
      ],
    );
  }

  Widget _modeRow(BuildContext context, String category, Map<String, dynamic> modes) {
    final t = AppL10n.of(context);
    return SettingsRow(
      label: _categoryLabel(t, category),
      description: _categoryHint(t, category),
      control: SegmentedButton<String>(
        showSelectedIcon: false,
        segments: <ButtonSegment<String>>[
          ButtonSegment<String>(value: 'off', label: Text(t.modeOff)),
          ButtonSegment<String>(value: 'restore', label: Text(t.modeRestore)),
          ButtonSegment<String>(value: 'hide', label: Text(t.modeHide)),
        ],
        selected: <String>{'${modes[category] ?? 'off'}'},
        onSelectionChanged: (value) => _update(() => modes[category] = value.first),
      ),
    );
  }

  static String _categoryLabel(AppL10n t, String category) => switch (category) {
        'email' => t.catEmail,
        'phone' => t.catPhone,
        'iban' => t.catIban,
        'card' => t.catCard,
        'ip' => t.catIp,
        'secret' => t.catSecret,
        'custom' => t.catCustom,
        'person' => t.catPerson,
        'address' => t.catAddress,
        'organization' => t.catOrganization,
        'id' => t.catId,
        _ => category,
      };

  static String _categoryHint(AppL10n t, String category) => switch (category) {
        'email' => t.catEmailHint,
        'phone' => t.catPhoneHint,
        'iban' => t.catIbanHint,
        'card' => t.catCardHint,
        'ip' => t.catIpHint,
        'secret' => t.catSecretHint,
        'custom' => t.catCustomHint,
        'person' => t.catPersonHint,
        'address' => t.catAddressHint,
        'organization' => t.catOrganizationHint,
        'id' => t.catIdHint,
        _ => '',
      };

  // ---- compression ---------------------------------------------------------

  Widget _compressionView(BuildContext context) {
    final t = AppL10n.of(context);
    final level = '${_compression['level'] ?? 'off'}';
    final engines = _compression['engines'] as Map<String, dynamic>;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        MetricStrip(
          metrics: <({String label, String value, String? caption})>[
            (label: t.compressionSaved, value: formatCount((_stats['savedTokens'] as num?) ?? 0), caption: t.last30Days),
            (label: t.compressionRequests, value: formatCount((_stats['compressedRequests'] as num?) ?? 0), caption: t.ofRequests(formatCount((_stats['requests'] as num?) ?? 0))),
          ],
        ),
        const SizedBox(height: 28),
        SettingsHeading(title: t.compressionLevel, description: t.compressionLevelHint),
        for (final option in <String>['off', 'lossless', 'balanced', 'aggressive'])
          _LevelCard(
            title: _levelLabel(t, option),
            description: _levelHint(t, option),
            selected: level == option,
            onTap: () => _update(() => _compression['level'] = option),
          ),
        const SizedBox(height: 20),
        if (level != 'off')
          _Expandable(
            title: t.advancedOptions,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: <Widget>[
                SettingsGroup(
                  title: t.compressionEngines,
                  description: t.compressionEnginesHint,
                  children: <Widget>[
                    for (final entry in _engines.entries)
                      if (_levelRank('${entry.value}') <= _levelRank(level))
                        SettingsRow(
                          label: _engineLabel(t, entry.key),
                          description: _engineHint(t, entry.key),
                          control: Switch(value: engines[entry.key] != false, onChanged: (value) => _update(() => engines[entry.key] = value)),
                        ),
                  ],
                ),
                SettingsGroup(
                  title: t.compressionLimits,
                  children: <Widget>[
                    _numberRow(t.compressionMinTokens, t.compressionMinTokensHint, 'minTokens'),
                    _numberRow(t.compressionKeepRecent, t.compressionKeepRecentHint, 'keepRecent'),
                    if (level == 'aggressive') _numberRow(t.compressionTarget, t.compressionTargetHint, 'targetTokens', nullable: true),
                  ],
                ),
              ],
            ),
          ),
        _TryIt(
          key: const ValueKey<String>('compression'),
          controller: widget.controller,
          settings: _body,
          role: 'tool',
          hint: t.compressionTryHint,
          sample: _compressionSample,
          describe: (result) {
            final stats = Map<String, dynamic>.from((result['stats'] as Map)['compression'] as Map);
            return t.compressionTryResult('${stats['beforeTokens']}', '${stats['afterTokens']}');
          },
        ),
      ],
    );
  }

  Widget _numberRow(String label, String hint, String field, {bool nullable = false}) {
    return SettingsRow(
      label: label,
      description: hint,
      control: SizedBox(
        width: 120,
        child: TextFormField(
          initialValue: _compression[field] == null ? '' : '${_compression[field]}',
          keyboardType: TextInputType.number,
          onChanged: (value) {
            final number = int.tryParse(value.trim());
            if (number == null && !nullable) return;
            _update(() => _compression[field] = number);
          },
        ),
      ),
    );
  }

  static int _levelRank(String level) => <String>['off', 'lossless', 'balanced', 'aggressive'].indexOf(level);

  static String _levelLabel(AppL10n t, String level) => switch (level) {
        'lossless' => t.levelLossless,
        'balanced' => t.levelBalanced,
        'aggressive' => t.levelAggressive,
        _ => t.levelOff,
      };

  static String _levelHint(AppL10n t, String level) => switch (level) {
        'lossless' => t.levelLosslessHint,
        'balanced' => t.levelBalancedHint,
        'aggressive' => t.levelAggressiveHint,
        _ => t.levelOffHint,
      };

  static String _engineLabel(AppL10n t, String engine) => switch (engine) {
        'whitespace' => t.engWhitespace,
        'jsonMinify' => t.engJsonMinify,
        'dedupe' => t.engDedupe,
        'repeatedLines' => t.engRepeatedLines,
        'jsonTables' => t.engJsonTables,
        'trimToolOutput' => t.engTrimToolOutput,
        'maskOldToolOutput' => t.engMaskOldToolOutput,
        'condenseHistory' => t.engCondenseHistory,
        'tokenTarget' => t.engTokenTarget,
        _ => engine,
      };

  static String _engineHint(AppL10n t, String engine) => switch (engine) {
        'whitespace' => t.engWhitespaceHint,
        'jsonMinify' => t.engJsonMinifyHint,
        'dedupe' => t.engDedupeHint,
        'repeatedLines' => t.engRepeatedLinesHint,
        'jsonTables' => t.engJsonTablesHint,
        'trimToolOutput' => t.engTrimToolOutputHint,
        'maskOldToolOutput' => t.engMaskOldToolOutputHint,
        'condenseHistory' => t.engCondenseHistoryHint,
        'tokenTarget' => t.engTokenTargetHint,
        _ => '',
      };
}

class _LevelCard extends StatelessWidget {
  const _LevelCard({required this.title, required this.description, required this.selected, required this.onTap});

  final String title;
  final String description;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final radius = BorderRadius.circular(AppRadius.card);
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Material(
        color: selected ? palette.accentSoft : palette.surface,
        shape: RoundedRectangleBorder(borderRadius: radius, side: BorderSide(color: selected ? palette.accent.withValues(alpha: 0.5) : palette.border)),
        child: InkWell(
          borderRadius: radius,
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Icon(selected ? Icons.radio_button_checked_rounded : Icons.radio_button_off_rounded, size: 18, color: selected ? palette.accent : palette.textMuted),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      Text(title, style: TextStyle(fontWeight: FontWeight.w600, color: palette.text)),
                      const SizedBox(height: 3),
                      Text(description, style: TextStyle(fontSize: 13, color: palette.textSoft, height: 1.45)),
                    ],
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _Expandable extends StatefulWidget {
  const _Expandable({required this.title, required this.child});

  final String title;
  final Widget child;

  @override
  State<_Expandable> createState() => _ExpandableState();
}

class _ExpandableState extends State<_Expandable> {
  bool _open = false;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Align(
          alignment: Alignment.centerLeft,
          child: TextButton.icon(
            onPressed: () => setState(() => _open = !_open),
            icon: Icon(_open ? Icons.expand_less_rounded : Icons.expand_more_rounded, size: 18),
            label: Text(widget.title),
          ),
        ),
        if (_open) ...<Widget>[const SizedBox(height: 8), widget.child],
        const SizedBox(height: 12),
      ],
    );
  }
}

/// Runs the current (unsaved) settings on a sample text; nothing is sent to
/// any model except the optional AI detection.
class _TryIt extends StatefulWidget {
  const _TryIt({super.key, required this.controller, required this.settings, required this.hint, required this.sample, required this.describe, this.role = 'user'});

  final NeoRouterController controller;
  final Map<String, Object?> Function() settings;
  final String hint;
  final String sample;
  final String Function(Map<String, dynamic>) describe;
  final String role;

  @override
  State<_TryIt> createState() => _TryItState();
}

class _TryItState extends State<_TryIt> {
  late final TextEditingController _input = TextEditingController(text: widget.sample);
  Map<String, dynamic>? _result;
  bool _running = false;
  String? _error;

  @override
  void dispose() {
    _input.dispose();
    super.dispose();
  }

  Future<void> _run() async {
    setState(() {
      _running = true;
      _error = null;
    });
    try {
      final result = await widget.controller.api.processingPreview(_input.text, widget.settings(), role: widget.role);
      setState(() => _result = result);
    } on ApiException catch (error) {
      setState(() => _error = error.message);
    } finally {
      if (mounted) setState(() => _running = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final messages = ((_result?['messages'] as List?) ?? const <Object>[]).cast<Map>();
    final output = messages.isEmpty ? '' : '${messages.last['content']}';
    return SettingsGroup(
      title: t.tryItTitle,
      description: widget.hint,
      children: <Widget>[
        Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              TextField(controller: _input, minLines: 4, maxLines: 12, style: const TextStyle(fontSize: 13.5, height: 1.45)),
              const SizedBox(height: 12),
              Row(
                children: <Widget>[
                  OutlinedButton.icon(
                    onPressed: _running ? null : _run,
                    icon: _running ? const SizedBox.square(dimension: 14, child: CircularProgressIndicator(strokeWidth: 2)) : const Icon(Icons.play_arrow_rounded, size: 18),
                    label: Text(t.tryItRun),
                  ),
                  const SizedBox(width: 12),
                  if (_result != null) Expanded(child: Text(widget.describe(_result!), style: TextStyle(color: palette.textSoft, fontSize: 13))),
                ],
              ),
              if (_error != null) ...<Widget>[const SizedBox(height: 12), InlineError(message: _error!)],
              if (_result != null) ...<Widget>[
                const SizedBox(height: 12),
                Text(t.tryItSent, style: TextStyle(color: palette.textMuted, fontSize: 12.5)),
                const SizedBox(height: 6),
                Container(
                  padding: const EdgeInsets.all(14),
                  decoration: BoxDecoration(color: palette.surfaceRaised, borderRadius: BorderRadius.circular(AppRadius.control)),
                  child: SelectableText(output, style: GoogleFonts.geistMono(fontSize: 12.5, height: 1.55, color: palette.text)),
                ),
              ],
            ],
          ),
        ),
      ],
    );
  }
}
