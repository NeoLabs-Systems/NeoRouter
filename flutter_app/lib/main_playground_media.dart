import 'dart:async';
import 'dart:convert';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/web_media.dart';

/// The playground's non-chat modes. One state serves every kind, so audio
/// read aloud in Speech can be transcribed again in Transcription.
class MediaPlayground extends StatefulWidget {
  const MediaPlayground({super.key, required this.controller, required this.kind, required this.routing, required this.controls, required this.hasModels});

  final NeoRouterController controller;
  final String kind;

  /// keyId, model and freeOnly for the next request.
  final Map<String, Object?> Function() routing;
  final Widget controls;
  final bool hasModels;

  @override
  State<MediaPlayground> createState() => _MediaPlaygroundState();
}

class _MediaPlaygroundState extends State<MediaPlayground> {
  final Map<String, TextEditingController> _inputs = <String, TextEditingController>{};
  final TextEditingController _voice = TextEditingController();
  final Map<String, Map<String, dynamic>> _results = <String, Map<String, dynamic>>{};
  final Map<String, String> _errors = <String, String>{};
  final Map<String, int> _latency = <String, int>{};
  String _size = '1024x1024';
  int _seconds = 4;
  bool _translate = false;
  String _check = 'safety';
  bool _running = false;
  PickedFile? _file;
  ({Uint8List bytes, String mime})? _spoken;
  AudioHandle? _playing;
  Timer? _poll;

  TextEditingController _input(String kind) => _inputs.putIfAbsent(kind, TextEditingController.new);

  @override
  void dispose() {
    for (final controller in _inputs.values) {
      controller.dispose();
    }
    _voice.dispose();
    _playing?.stop();
    _poll?.cancel();
    super.dispose();
  }

  Future<void> _run() async {
    final kind = widget.kind;
    final text = _input(kind).text.trim();
    final api = widget.controller.api;
    final body = <String, Object?>{...widget.routing()};
    switch (kind) {
      case 'embedding':
        final lines = text.split('\n').map((line) => line.trim()).where((line) => line.isNotEmpty).toList();
        if (lines.isEmpty) return;
        body['input'] = lines;
      case 'image':
        if (text.isEmpty) return;
        body.addAll(<String, Object?>{'prompt': text, 'size': _size});
      case 'speech':
        if (text.isEmpty) return;
        body.addAll(<String, Object?>{'input': text, 'voice': _voice.text.trim().isEmpty ? null : _voice.text.trim()});
      case 'transcription':
        final audio = _file?.bytes ?? _spoken?.bytes;
        if (audio == null) return;
        body.addAll(<String, Object?>{
          'audio': base64Encode(audio),
          'filename': _file?.name ?? 'speech.wav',
          'mime': _file?.mime ?? _spoken?.mime,
          'translate': _translate,
        });
      case 'moderation':
        if (text.isEmpty) return;
        body.addAll(<String, Object?>{'input': text, 'check': _check});
      case 'video':
        if (text.isEmpty) return;
        body.addAll(<String, Object?>{'prompt': text, 'seconds': _seconds});
    }
    setState(() {
      _running = true;
      _errors.remove(kind);
    });
    final started = DateTime.now();
    try {
      final result = await api.playgroundMedia(kind, body);
      if (!mounted) return;
      setState(() {
        _results[kind] = result;
        _latency[kind] = DateTime.now().difference(started).inMilliseconds;
        if (kind == 'speech') _spoken = (bytes: base64Decode(result['audio'] as String), mime: '${result['mime']}');
      });
      if (kind == 'video') _watchVideo('${result['id']}');
    } on ApiException catch (error) {
      if (mounted) setState(() => _errors[kind] = error.message);
    } finally {
      if (mounted) setState(() => _running = false);
    }
  }

  void _watchVideo(String id) {
    _poll?.cancel();
    _poll = Timer.periodic(const Duration(seconds: 5), (timer) async {
      try {
        final job = await widget.controller.api.playgroundVideo(id);
        if (!mounted) return timer.cancel();
        setState(() => _results['video'] = job);
        if (job['status'] == 'completed' || job['status'] == 'failed') timer.cancel();
      } on ApiException {
        timer.cancel();
      }
    });
  }

  Future<void> _downloadVideo(String id) async {
    final content = await widget.controller.api.playgroundVideoContent(id);
    downloadBytes(base64Decode(content['video'] as String), '${content['mime']}', '$id.mp4');
  }

  void _togglePlay() {
    final spoken = _spoken;
    if (_playing != null) {
      _playing!.stop();
      setState(() => _playing = null);
      return;
    }
    if (spoken == null) return;
    final handle = playAudio(spoken.bytes, spoken.mime);
    setState(() => _playing = handle);
    handle.done.future.then((_) {
      if (mounted && identical(_playing, handle)) setState(() => _playing = null);
    });
  }

  Future<void> _chooseFile() async {
    final picked = await pickFile(accept: 'audio/*,video/mp4,video/webm');
    if (picked != null) setState(() => _file = picked);
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final kind = widget.kind;
    if (!widget.hasModels) {
      return Center(
        child: EmptyState(
          icon: Icons.extension_outlined,
          title: t.pgNoModels,
          message: '',
          action: FilledButton(onPressed: () => widget.controller.go(AppPage.providers), child: Text(t.pgAddProvider)),
        ),
      );
    }
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 780),
        child: ListView(
          padding: const EdgeInsets.only(bottom: 24),
          children: <Widget>[
            Text(_hint(t), style: TextStyle(color: palette.textSoft, height: 1.45)),
            const SizedBox(height: AppSpacing.md),
            AppCard(
              padding: const EdgeInsets.fromLTRB(16, 8, 12, 12),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: <Widget>[
                  if (kind == 'transcription') _audioSource(t, palette) else _textInput(t),
                  const SizedBox(height: AppSpacing.sm),
                  _options(t),
                  const SizedBox(height: AppSpacing.sm),
                  Row(
                    children: <Widget>[
                      Expanded(child: widget.controls),
                      const SizedBox(width: 10),
                      FilledButton(
                        onPressed: _running ? null : _run,
                        child: _running ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2)) : Text(_runLabel(t)),
                      ),
                    ],
                  ),
                ],
              ),
            ),
            if (_errors[kind] != null) ...<Widget>[
              const SizedBox(height: AppSpacing.md),
              Text(_errors[kind]!, style: TextStyle(color: palette.error, height: 1.45)),
            ],
            if (_results[kind] != null) ...<Widget>[const SizedBox(height: AppSpacing.lg), _result(context, kind, _results[kind]!)],
          ],
        ),
      ),
    );
  }

  String _hint(AppL10n t) => switch (widget.kind) {
    'embedding' => t.pgEmbedHint,
    'image' => t.pgImageHint,
    'speech' => t.pgSpeechHint,
    'transcription' => t.pgTranscribeHint,
    'moderation' => t.pgCheckHint,
    _ => t.pgVideoHint,
  };

  String _runLabel(AppL10n t) => switch (widget.kind) {
    'embedding' => t.pgEmbedRun,
    'image' => t.pgImageRun,
    'speech' => t.pgSpeechRun,
    'transcription' => t.pgTranscribeRun,
    'moderation' => t.pgCheckRun,
    _ => t.pgVideoRun,
  };

  Widget _textInput(AppL10n t) => CallbackShortcuts(
    bindings: <ShortcutActivator, VoidCallback>{const SingleActivator(LogicalKeyboardKey.enter, meta: true): _run},
    child: TextField(
      controller: _input(widget.kind),
      minLines: widget.kind == 'embedding' ? 3 : 2,
      maxLines: 10,
      style: const TextStyle(fontSize: 14.5, height: 1.45),
      decoration: const InputDecoration(filled: false, border: InputBorder.none, enabledBorder: InputBorder.none, focusedBorder: InputBorder.none),
    ),
  );

  Widget _audioSource(AppL10n t, NeoRouterPalette palette) => Padding(
    padding: const EdgeInsets.only(top: 8),
    child: Wrap(
      spacing: 8,
      runSpacing: 8,
      crossAxisAlignment: WrapCrossAlignment.center,
      children: <Widget>[
        OutlinedButton.icon(onPressed: _chooseFile, icon: const Icon(Icons.upload_file_outlined, size: 17), label: Text(t.pgChooseFile)),
        if (_spoken != null) ChoiceChip(label: Text(t.pgUseSpeech), selected: _file == null, onSelected: (_) => setState(() => _file = null)),
        if (_file != null)
          Text('${_file!.name} · ${(_file!.bytes.length / 1024).toStringAsFixed(0)} KB', style: TextStyle(color: palette.textSoft, fontSize: 13)),
      ],
    ),
  );

  Widget _options(AppL10n t) {
    switch (widget.kind) {
      case 'image':
        return Wrap(
          spacing: 6,
          children: <Widget>[
            for (final size in const <String>['1024x1024', '1536x1024', '1024x1536'])
              ChoiceChip(label: Text(size.replaceAll('x', ' × ')), selected: _size == size, onSelected: (_) => setState(() => _size = size)),
          ],
        );
      case 'speech':
        return SizedBox(
          width: 220,
          child: TextField(
            controller: _voice,
            decoration: InputDecoration(isDense: true, labelText: t.pgVoice, hintText: t.pgVoiceDefault),
          ),
        );
      case 'transcription':
        return Align(
          alignment: Alignment.centerLeft,
          child: FilterChip(label: Text(t.pgTranslate), selected: _translate, onSelected: (value) => setState(() => _translate = value)),
        );
      case 'moderation':
        return Wrap(
          spacing: 6,
          children: <Widget>[
            for (final (value, label) in <(String, String)>[('safety', t.pgCheckSafety), ('prompt_injection', t.pgCheckInjection)])
              ChoiceChip(label: Text(label), selected: _check == value, onSelected: (_) => setState(() => _check = value)),
          ],
        );
      case 'video':
        return Wrap(
          spacing: 6,
          children: <Widget>[
            for (final seconds in const <int>[4, 8, 12])
              ChoiceChip(label: Text(t.pgSeconds('$seconds')), selected: _seconds == seconds, onSelected: (_) => setState(() => _seconds = seconds)),
          ],
        );
      default:
        return const SizedBox.shrink();
    }
  }

  Widget _meta(String? model) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    if (model == null) return const SizedBox.shrink();
    return Padding(
      padding: const EdgeInsets.only(top: 10),
      child: Text(t.routedTo(shortModelName(model), formatMs(_latency[widget.kind])), style: TextStyle(color: palette.textMuted, fontSize: 12)),
    );
  }

  Widget _result(BuildContext context, String kind, Map<String, dynamic> result) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final model = result['model'] as String?;
    switch (kind) {
      case 'embedding':
        final vectors = (result['data'] as List).map((item) => ((item as Map)['embedding'] as List).cast<num>()).toList();
        final labels = _input('embedding').text.split('\n').map((line) => line.trim()).where((line) => line.isNotEmpty).toList();
        return AppCard(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Text(t.pgEmbedResult('${vectors.length}', '${vectors.first.length}'), style: const TextStyle(fontWeight: FontWeight.w600)),
              const SizedBox(height: 6),
              SelectableText(
                '[${vectors.first.take(6).map((value) => value.toStringAsFixed(4)).join(', ')}, …]',
                style: TextStyle(fontFamily: 'monospace', fontSize: 12.5, color: palette.textSoft),
              ),
              if (vectors.length > 1) ...<Widget>[
                const SizedBox(height: AppSpacing.md),
                Text(t.pgSimilarity, style: const TextStyle(fontWeight: FontWeight.w600)),
                const SizedBox(height: 6),
                for (var i = 0; i < vectors.length && i < 6; i++)
                  for (var j = i + 1; j < vectors.length && j < 6; j++)
                    _SimilarityRow(
                      a: labels.elementAtOrNull(i) ?? '#${i + 1}',
                      b: labels.elementAtOrNull(j) ?? '#${j + 1}',
                      value: _cosine(vectors[i], vectors[j]),
                    ),
              ],
              if (model != null) ...<Widget>[
                const SizedBox(height: AppSpacing.sm),
                InlineHint(message: t.pgPinned(shortModelName(model)), icon: Icons.push_pin_outlined),
              ],
              _meta(model),
            ],
          ),
        );
      case 'image':
        final images = (result['data'] as List).map((item) => base64Decode((item as Map)['b64_json'] as String)).toList();
        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            for (final (index, bytes) in images.indexed)
              Padding(
                padding: const EdgeInsets.only(bottom: 12),
                child: Stack(
                  children: <Widget>[
                    ClipRRect(
                      borderRadius: BorderRadius.circular(AppRadius.card),
                      child: Image.memory(bytes, fit: BoxFit.contain),
                    ),
                    Positioned(
                      right: 8,
                      top: 8,
                      child: IconButton.filledTonal(
                        tooltip: t.pgDownload,
                        onPressed: () => downloadBytes(bytes, 'image/png', 'neorouter-${index + 1}.png'),
                        icon: const Icon(Icons.download_rounded, size: 18),
                      ),
                    ),
                  ],
                ),
              ),
            _meta(model),
          ],
        );
      case 'speech':
        return AppCard(
          child: Row(
            children: <Widget>[
              FilledButton.tonalIcon(
                onPressed: _togglePlay,
                icon: Icon(_playing == null ? Icons.play_arrow_rounded : Icons.stop_rounded, size: 18),
                label: Text(_playing == null ? t.pgPlay : t.pgStop),
              ),
              const SizedBox(width: 8),
              TextButton.icon(
                onPressed: _spoken == null ? null : () => downloadBytes(_spoken!.bytes, _spoken!.mime, 'speech.${result['format'] ?? 'audio'}'),
                icon: const Icon(Icons.download_rounded, size: 17),
                label: Text(t.pgDownload),
              ),
              const Spacer(),
              Flexible(child: _meta(model)),
            ],
          ),
        );
      case 'transcription':
        final text = '${result['text'] ?? ''}';
        return AppCard(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Row(
                children: <Widget>[
                  Expanded(child: SelectableText(text, style: const TextStyle(fontSize: 14.5, height: 1.55))),
                  IconButton(
                    tooltip: t.copy,
                    onPressed: () => Clipboard.setData(ClipboardData(text: text)),
                    icon: const Icon(Icons.copy_rounded, size: 17),
                  ),
                ],
              ),
              _meta(model),
            ],
          ),
        );
      case 'moderation':
        final verdict = Map<String, dynamic>.from((result['results'] as List).first as Map);
        final flagged = verdict['flagged'] == true;
        final categories = Map<String, dynamic>.from(verdict['categories'] as Map? ?? const <String, dynamic>{});
        final hits = categories.entries.where((entry) => entry.value == true).map((entry) => entry.key).toList();
        return AppCard(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Row(
                children: <Widget>[
                  Icon(flagged ? Icons.gpp_maybe_outlined : Icons.verified_user_outlined, color: flagged ? palette.error : palette.success, size: 20),
                  const SizedBox(width: 8),
                  Text(flagged ? t.pgFlagged : t.pgSafe, style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 15)),
                ],
              ),
              if (hits.isNotEmpty) ...<Widget>[
                const SizedBox(height: 10),
                Wrap(spacing: 6, runSpacing: 6, children: <Widget>[for (final hit in hits) MetaPill(label: hit.replaceAll('_', ' '))]),
              ],
              if (verdict['rationale'] != null) ...<Widget>[
                const SizedBox(height: 10),
                Text('${verdict['rationale']}', style: TextStyle(color: palette.textSoft, height: 1.45)),
              ],
              _meta(model),
            ],
          ),
        );
      default:
        final status = '${result['status']}';
        final done = status == 'completed';
        return AppCard(
          child: Row(
            children: <Widget>[
              if (!done && status != 'failed') ...<Widget>[
                const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2)),
                const SizedBox(width: 12),
              ],
              Expanded(
                child: Text(t.pgVideoStatus(status.replaceAll('_', ' ')), style: TextStyle(color: status == 'failed' ? palette.error : palette.text)),
              ),
              if (done)
                FilledButton.tonalIcon(
                  onPressed: () => _downloadVideo('${result['id']}'),
                  icon: const Icon(Icons.download_rounded, size: 17),
                  label: Text(t.pgDownload),
                ),
            ],
          ),
        );
    }
  }

  static double _cosine(List<num> a, List<num> b) {
    var dot = 0.0;
    var na = 0.0;
    var nb = 0.0;
    for (var i = 0; i < a.length && i < b.length; i++) {
      dot += a[i] * b[i];
      na += a[i] * a[i];
      nb += b[i] * b[i];
    }
    return na == 0 || nb == 0 ? 0 : dot / (math.sqrt(na) * math.sqrt(nb));
  }
}

class _SimilarityRow extends StatelessWidget {
  const _SimilarityRow({required this.a, required this.b, required this.value});

  final String a;
  final String b;
  final double value;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Row(
        children: <Widget>[
          Expanded(
            child: Text(
              '$a ↔ $b',
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(color: palette.textSoft, fontSize: 13),
            ),
          ),
          const SizedBox(width: 12),
          SizedBox(
            width: 120,
            child: ClipRRect(
              borderRadius: BorderRadius.circular(AppRadius.pill),
              child: LinearProgressIndicator(value: value.clamp(0, 1), minHeight: 6, backgroundColor: palette.surfaceRaised, color: palette.accent),
            ),
          ),
          const SizedBox(width: 10),
          SizedBox(
            width: 44,
            child: Text(
              value.toStringAsFixed(2),
              textAlign: TextAlign.end,
              style: const TextStyle(fontSize: 13, fontFeatures: <FontFeature>[FontFeature.tabularFigures()]),
            ),
          ),
        ],
      ),
    );
  }
}
