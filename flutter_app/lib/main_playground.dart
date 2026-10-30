import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:url_launcher/url_launcher.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_model_picker.dart';
import 'main_playground_media.dart';
import 'main_routing.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/capabilities.dart';
import 'src/models.dart';

class _ChatMessage {
  _ChatMessage(this.role, this.content, {this.meta, this.problem});

  final String role;
  final String content;
  final String? meta;
  final ApiException? problem;

  bool get error => problem != null;
}

/// Try NeoRouter with one of your keys. One mode per kind of model the
/// user's connections offer (chat, embeddings, images, speech, …).
class PlaygroundScreen extends StatefulWidget {
  const PlaygroundScreen({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<PlaygroundScreen> createState() => _PlaygroundScreenState();
}

class _PlaygroundScreenState extends State<PlaygroundScreen> {
  List<RouterKey> _keys = <RouterKey>[];
  List<CapabilitySummary> _modes = <CapabilitySummary>[];
  String _mode = 'chat';
  String? _keyId;
  bool _freeOnly = true;
  bool _showLockHint = false;
  String _model = 'auto';
  final TextEditingController _system = TextEditingController();
  final TextEditingController _input = TextEditingController();
  final FocusNode _inputFocus = FocusNode();
  final ScrollController _scroll = ScrollController();
  final List<_ChatMessage> _messages = <_ChatMessage>[];
  bool _sending = false;
  bool _loading = true;
  // Which layout was last built, to notice when a resize flips it.
  bool? _rail;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _system.dispose();
    _input.dispose();
    _inputFocus.dispose();
    _scroll.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final api = widget.controller.api;
      final results = await Future.wait(<Future<Object>>[api.keys(), api.capabilities(all: true)]);
      final keys = (results[0] as List<RouterKey>).where((key) => !key.revoked).toList();
      setState(() {
        _keys = keys;
        _keyId = keys.isEmpty ? null : keys.first.id;
        // Every routable kind, set up or not, so each one can be tried or
        // connected from here. Live voice is a WebSocket for apps, not a playground.
        _modes = sortCapabilities((results[1] as List<CapabilitySummary>).where((item) => item.routable && item.id != 'live').toList());
        if (!_modes.any((mode) => mode.id == _mode)) _mode = 'chat';
        // ?page=playground&mode=image opens a mode directly.
        final linked = Uri.base.queryParameters['mode'];
        if (linked != null && _modes.any((mode) => mode.id == linked)) _mode = linked;
      });
    } catch (_) {
      // Without capabilities the chat mode still works.
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  void _scrollToEnd() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_scroll.hasClients) _scroll.animateTo(_scroll.position.maxScrollExtent, duration: const Duration(milliseconds: 200), curve: Curves.easeOut);
    });
  }

  Future<void> _send([String? preset]) async {
    final text = (preset ?? _input.text).trim();
    if (text.isEmpty || _keyId == null || _sending) return;
    setState(() {
      _messages.add(_ChatMessage('user', text));
      _input.clear();
      _sending = true;
    });
    _scrollToEnd();
    final history = <Map<String, String>>[
      if (_system.text.trim().isNotEmpty) <String, String>{'role': 'system', 'content': _system.text.trim()},
      for (final message in _messages.where((item) => !item.error)) <String, String>{'role': message.role, 'content': message.content},
    ];
    final started = DateTime.now();
    try {
      final response = await widget.controller.api.playground(<String, Object?>{'keyId': _keyId, 'model': _model, 'freeOnly': _freeOnly, 'messages': history});
      final message = (response['choices'] as List).first['message'] as Map;
      final latency = DateTime.now().difference(started).inMilliseconds;
      if (!mounted) return;
      setState(
        () => _messages.add(
          _ChatMessage(
            'assistant',
            (message['content'] as String?) ?? '',
            meta: AppL10n.of(context).routedTo(shortModelName('${response['model']}'), formatMs(latency)),
          ),
        ),
      );
    } on ApiException catch (error) {
      setState(() => _messages.add(_ChatMessage('assistant', error.message, problem: error)));
    } finally {
      if (mounted) setState(() => _sending = false);
      _scrollToEnd();
      _inputFocus.requestFocus();
    }
  }

  // Sends the last question again, without the failed answer.
  void _retryLast() {
    final last = _messages.lastWhere((message) => message.role == 'user', orElse: () => _ChatMessage('user', ''));
    if (last.content.isEmpty) return;
    setState(() {
      _messages.removeWhere((message) => message.problem != null);
      _messages.remove(last);
    });
    _send(last.content);
  }

  Future<void> _pickSpecificModel() async {
    final picked = await showModelPicker(
      context,
      controller: widget.controller,
      capability: _mode,
      selected: <String>[if (!_model.startsWith('auto')) _model],
    );
    if (picked != null && picked.isNotEmpty) setState(() => _model = picked.first);
  }

  Future<void> _editInstructions() async {
    final t = AppL10n.of(context);
    await showDialog<void>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text(t.systemPromptTitle),
        content: SizedBox(
          width: 480,
          child: TextField(
            controller: _system,
            autofocus: true,
            minLines: 4,
            maxLines: 10,
            decoration: InputDecoration(hintText: t.systemPromptHint),
          ),
        ),
        actions: <Widget>[FilledButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(t.close))],
      ),
    );
    setState(() {});
  }

  Widget _routingChips(BuildContext context, {bool wrap = false}) {
    final t = AppL10n.of(context);
    final key = _keys.where((item) => item.id == _keyId).firstOrNull;
    return _RoutingChips(
      wrap: wrap,
      modelLabel: _modelLabel(t),
      keyName: key?.name,
      keys: _keys,
      onModel: (value) => value == '__specific__' ? _pickSpecificModel() : setState(() => _model = value),
      onKey: (value) => setState(() {
        _keyId = value;
        _showLockHint = false;
      }),
      // A free-only key can never leave free mode.
      keyFreeOnly: key?.mode == 'free_only',
      freeOnly: _freeOnly,
      onFreeOnly: (value) => setState(() => _freeOnly = value),
      onLockedTap: () => setState(() => _showLockHint = !_showLockHint),
    );
  }

  String _modelLabel(AppL10n t) {
    if (_model == 'auto') return t.autoKeyDefault;
    if (_model.startsWith('auto:')) return presetLabel(t, _model.substring(5));
    return shortModelName(_model);
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    if (_loading) return const Center(child: SizedBox.square(dimension: 22, child: CircularProgressIndicator(strokeWidth: 2)));
    final padding = pagePadding(context);
    final mode = capabilityInfo(_mode);
    final content = _keys.isEmpty
        ? Center(
            child: EmptyState(
              icon: Icons.key_outlined,
              title: t.noKeysTitle,
              message: t.playgroundNeedsKey,
              action: FilledButton(onPressed: () => widget.controller.go(AppPage.keys), child: Text(t.createKey)),
            ),
          )
        : _mode == 'chat'
        ? _chat(context)
        : MediaPlayground(
            controller: widget.controller,
            kind: _mode,
            summary: _summary,
            model: _model,
            freeOnly: _effectiveFreeOnly,
            onAllowPaid: _allowPaid,
            onUseAuto: () => setState(() => _model = 'auto'),
            onConnected: _load,
            routing: () => <String, Object?>{'keyId': _keyId, 'model': _model, 'freeOnly': _freeOnly},
            controls: _routingChips(context, wrap: true),
          );
    return Padding(
      padding: EdgeInsets.fromLTRB(padding.left, padding.top, padding.right, 16),
      child: LayoutBuilder(
        builder: (context, box) {
          // Wide: the modes sit in a rail on the left. Narrow: one button opens them.
          final rail = box.maxWidth >= 860 && _modes.length > 1;
          // A menu opened from the other layout has lost its button: close it.
          if (_rail != null && _rail != rail) {
            WidgetsBinding.instance.addPostFrameCallback((_) {
              if (mounted) Navigator.of(context).popUntil((route) => route is! PopupRoute || route is RawDialogRoute);
            });
          }
          _rail = rail;
          return Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: <Widget>[
                        Text(t.playgroundTitle, style: displayTitleStyle(palette, size: 24)),
                        // The other modes explain themselves above their input.
                        if (_mode == 'chat') ...<Widget>[
                          const SizedBox(height: 4),
                          Text(mode.description(t), maxLines: 2, overflow: TextOverflow.ellipsis, style: TextStyle(color: palette.textSoft, height: 1.4)),
                        ],
                      ],
                    ),
                  ),
                  if (_keys.isNotEmpty && _mode == 'chat') ...<Widget>[
                    const SizedBox(width: 12),
                    if (_messages.isNotEmpty)
                      TextButton.icon(onPressed: () => setState(_messages.clear), icon: const Icon(Icons.add_comment_outlined, size: 17), label: Text(t.newChat)),
                    TextButton.icon(
                      onPressed: _editInstructions,
                      icon: Icon(Icons.tune_rounded, size: 17, color: _system.text.trim().isEmpty ? null : palette.accent),
                      label: Text(t.systemPromptTitle),
                    ),
                  ],
                ],
              ),
              if (!rail && _modes.length > 1) ...<Widget>[
                const SizedBox(height: AppSpacing.md),
                Align(alignment: Alignment.centerLeft, child: _ModeButton(modes: _modes, selected: _mode, onChanged: _switchMode)),
              ],
              const SizedBox(height: AppSpacing.lg),
              Expanded(
                // One Row in both layouts, content last, so a resize keeps
                // what was typed or shown.
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: <Widget>[
                    if (rail) ...<Widget>[
                      SizedBox(width: 200, child: _ModeRail(modes: _modes, selected: _mode, onChanged: _switchMode)),
                      const SizedBox(width: AppSpacing.xl),
                    ],
                    Expanded(key: const ValueKey<String>('content'), child: content),
                  ],
                ),
              ),
            ],
          );
        },
      ),
    );
  }

  CapabilitySummary? get _summary => _modes.where((mode) => mode.id == _mode).firstOrNull;

  bool get _keyFreeOnly => _keys.where((key) => key.id == _keyId).firstOrNull?.mode == 'free_only';

  bool get _effectiveFreeOnly => _freeOnly || _keyFreeOnly;

  // Null when the key itself only allows free models: then Free only stays.
  VoidCallback? get _allowPaid => _keyFreeOnly ? null : () => setState(() => _freeOnly = false);

  void _switchMode(String value) => setState(() {
    _mode = value;
    // A specific model belongs to one kind; each mode starts on auto.
    if (!_model.startsWith('auto')) _model = 'auto';
  });

  Widget _chat(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 780),
        child: Column(
          children: <Widget>[
            Expanded(
              child: _messages.isEmpty
                  ? _Welcome(onPick: _send)
                  : ListView(
                      controller: _scroll,
                      padding: const EdgeInsets.only(bottom: 12),
                      children: <Widget>[
                        for (final message in _messages)
                          if (message.problem != null)
                            Padding(
                              padding: const EdgeInsets.symmetric(vertical: 8),
                              child: PlaygroundProblem(
                                error: message.problem!,
                                controller: widget.controller,
                                freeOnly: _effectiveFreeOnly,
                                onAllowPaid: _allowPaid,
                                onUseAuto: () => setState(() => _model = 'auto'),
                                onRetry: _retryLast,
                              ),
                            )
                          else
                            _Bubble(message: message),
                        if (_sending)
                          Padding(
                            padding: const EdgeInsets.symmetric(vertical: 12),
                            child: Row(
                              children: <Widget>[
                                const SizedBox.square(dimension: 14, child: CircularProgressIndicator(strokeWidth: 2)),
                                const SizedBox(width: 10),
                                Text(t.thinking, style: TextStyle(color: palette.textMuted, fontSize: 13)),
                              ],
                            ),
                          ),
                      ],
                    ),
            ),
            if (_showLockHint && _keys.where((key) => key.id == _keyId).firstOrNull?.mode == 'free_only')
              Padding(
                padding: const EdgeInsets.only(bottom: AppSpacing.sm),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: <Widget>[
                    InlineHint(message: t.freeModeLockedExplain, icon: Icons.lock_outline_rounded),
                    Align(
                      alignment: Alignment.centerRight,
                      child: TextButton(onPressed: () => widget.controller.go(AppPage.keys), child: Text(t.navKeys)),
                    ),
                  ],
                ),
              ),
            if (_effectiveFreeOnly && _summary != null && _summary!.total > 0 && _summary!.free == 0)
              Padding(padding: const EdgeInsets.only(bottom: AppSpacing.sm), child: FreeOnlyNotice(onAllowPaid: _allowPaid)),
            _Composer(input: _input, focus: _inputFocus, sending: _sending, onSend: _send, chips: _routingChips(context, wrap: true)),
            if (MediaQuery.sizeOf(context).width >= AppBreakpoints.tablet)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(t.enterToSend, textAlign: TextAlign.center, style: GoogleFonts.geist(fontSize: 11.5, color: palette.textMuted)),
              ),
          ],
        ),
      ),
    );
  }
}

class _Welcome extends StatelessWidget {
  const _Welcome({required this.onPick});

  final ValueChanged<String> onPick;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final suggestions = <String>[t.suggestion1, t.suggestion2, t.suggestion3];
    return Center(
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            const BrandLockup(logoSize: 40, showName: false),
            const SizedBox(height: 18),
            Text(t.playgroundEmptyTitle, textAlign: TextAlign.center, style: displayTitleStyle(palette, size: 22)),
            const SizedBox(height: 8),
            ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 460),
              child: Text(
                t.playgroundEmptyHint,
                textAlign: TextAlign.center,
                style: TextStyle(color: palette.textSoft, height: 1.45),
              ),
            ),
            const SizedBox(height: 24),
            Wrap(
              alignment: WrapAlignment.center,
              spacing: 8,
              runSpacing: 8,
              children: <Widget>[for (final suggestion in suggestions) ActionChip(label: Text(suggestion), onPressed: () => onPick(suggestion))],
            ),
          ],
        ),
      ),
    );
  }
}

class _Bubble extends StatelessWidget {
  const _Bubble({required this.message});

  final _ChatMessage message;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final user = message.role == 'user';
    final text = SelectableText(message.content, style: TextStyle(color: message.error ? palette.error : palette.text, fontSize: 14.5, height: 1.55));
    if (user) {
      return Align(
        alignment: Alignment.centerRight,
        child: Container(
          constraints: const BoxConstraints(maxWidth: 560),
          margin: const EdgeInsets.only(top: 10, bottom: 10),
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 11),
          decoration: BoxDecoration(color: palette.surfaceRaised, borderRadius: BorderRadius.circular(18)),
          child: text,
        ),
      );
    }
    final t = AppL10n.of(context);
    return Padding(
      padding: const EdgeInsets.only(top: 8, bottom: 14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          _Answer(markdown: message.content),
          const SizedBox(height: 6),
          Row(
            children: <Widget>[
              if (message.meta != null) Flexible(child: Text(message.meta!, style: TextStyle(color: palette.textMuted, fontSize: 12))),
              IconButton(
                tooltip: t.copy,
                visualDensity: VisualDensity.compact,
                onPressed: () {
                  Clipboard.setData(ClipboardData(text: message.content));
                  showSnack(context, t.copied);
                },
                icon: Icon(Icons.copy_rounded, size: 15, color: palette.textMuted),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

/// An answer as Markdown: bold, lists, headings, links, tables and code, in
/// the app's type and colours.
class _Answer extends StatelessWidget {
  const _Answer({required this.markdown});

  final String markdown;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final body = TextStyle(color: palette.text, fontSize: 14.5, height: 1.6);
    final mono = GoogleFonts.geistMono(fontSize: 13, height: 1.5, color: palette.text);
    return MarkdownBody(
      data: markdown,
      selectable: true,
      onTapLink: (_, href, _) {
        if (href != null) launchUrl(Uri.parse(href));
      },
      styleSheet: MarkdownStyleSheet(
        p: body,
        strong: body.copyWith(fontWeight: FontWeight.w700),
        em: body.copyWith(fontStyle: FontStyle.italic),
        a: body.copyWith(color: palette.accent, decoration: TextDecoration.underline, decorationColor: palette.accent),
        h1: displayTitleStyle(palette, size: 22),
        h2: displayTitleStyle(palette, size: 19),
        h3: body.copyWith(fontSize: 16, fontWeight: FontWeight.w700),
        h4: body.copyWith(fontWeight: FontWeight.w700),
        h1Padding: const EdgeInsets.only(top: 8),
        h2Padding: const EdgeInsets.only(top: 8),
        h3Padding: const EdgeInsets.only(top: 6),
        blockSpacing: 10,
        listBullet: body,
        listIndent: 22,
        code: mono.copyWith(backgroundColor: palette.surfaceRaised, fontSize: 13),
        codeblockPadding: const EdgeInsets.all(14),
        codeblockDecoration: BoxDecoration(
          color: palette.surfaceRaised,
          borderRadius: BorderRadius.circular(AppRadius.control),
          border: Border.all(color: palette.border),
        ),
        blockquote: body.copyWith(color: palette.textSoft),
        blockquotePadding: const EdgeInsets.fromLTRB(14, 4, 8, 4),
        blockquoteDecoration: BoxDecoration(border: Border(left: BorderSide(color: palette.accent.withValues(alpha: 0.6), width: 3))),
        tableHead: body.copyWith(fontWeight: FontWeight.w600),
        tableBody: body,
        tableBorder: TableBorder.all(color: palette.border),
        tableCellsPadding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
        horizontalRuleDecoration: BoxDecoration(border: Border(top: BorderSide(color: palette.border))),
      ),
    );
  }
}

class _Composer extends StatelessWidget {
  const _Composer({required this.input, required this.focus, required this.sending, required this.onSend, required this.chips});

  final TextEditingController input;
  final FocusNode focus;
  final bool sending;
  final Future<void> Function([String?]) onSend;
  final Widget chips;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return Container(
      padding: const EdgeInsets.fromLTRB(16, 6, 8, 8),
      decoration: BoxDecoration(
        color: palette.surface,
        borderRadius: BorderRadius.circular(20),
        border: Border.all(color: palette.borderStrong),
        boxShadow: <BoxShadow>[BoxShadow(color: palette.shadow, blurRadius: 20, offset: const Offset(0, 6))],
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          CallbackShortcuts(
            bindings: <ShortcutActivator, VoidCallback>{const SingleActivator(LogicalKeyboardKey.enter): () => onSend()},
            child: TextField(
              controller: input,
              focusNode: focus,
              autofocus: true,
              minLines: 1,
              maxLines: 8,
              keyboardType: TextInputType.multiline,
              style: const TextStyle(fontSize: 14.5, height: 1.45),
              decoration: InputDecoration(
                hintText: t.messageHint,
                filled: false,
                border: InputBorder.none,
                enabledBorder: InputBorder.none,
                focusedBorder: InputBorder.none,
                contentPadding: const EdgeInsets.symmetric(vertical: 12),
              ),
            ),
          ),
          Row(
            crossAxisAlignment: CrossAxisAlignment.end,
            children: <Widget>[
              Expanded(child: chips),
              const SizedBox(width: 10),
              IconButton.filled(
                tooltip: t.enterToSend,
                onPressed: sending ? null : () => onSend(),
                style: IconButton.styleFrom(backgroundColor: palette.accent, foregroundColor: palette.onAccent, disabledBackgroundColor: palette.surfaceRaised),
                icon: const Icon(Icons.arrow_upward_rounded, size: 19),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

/// Model, key and free-mode chips, shared by chat and the media modes.
class _RoutingChips extends StatelessWidget {
  const _RoutingChips({
    required this.modelLabel,
    required this.keyName,
    required this.keys,
    required this.onModel,
    required this.onKey,
    required this.keyFreeOnly,
    required this.freeOnly,
    required this.onFreeOnly,
    required this.onLockedTap,
    this.wrap = false,
  });

  final String modelLabel;
  final String? keyName;
  final List<RouterKey> keys;
  final ValueChanged<String> onModel;
  final ValueChanged<String> onKey;
  final bool keyFreeOnly;
  final bool freeOnly;
  final ValueChanged<bool> onFreeOnly;
  final VoidCallback onLockedTap;
  final bool wrap;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final children = <Widget>[
      PopupMenuButton<String>(
        tooltip: t.modelPicker,
        onSelected: onModel,
        itemBuilder: (_) => <PopupMenuEntry<String>>[
          // Plain auto follows the key's own profile.
          PopupMenuItem<String>(
            value: 'auto',
            child: Row(
              children: <Widget>[
                Icon(Icons.auto_awesome_outlined, size: 17, color: palette.textSoft),
                const SizedBox(width: 12),
                Text(t.autoKeyDefault),
              ],
            ),
          ),
          for (final id in presetOrder)
            PopupMenuItem<String>(
              value: 'auto:$id',
              child: Row(
                children: <Widget>[
                  Icon(presetIcon(id), size: 17, color: palette.textSoft),
                  const SizedBox(width: 12),
                  Text(presetLabel(t, id)),
                ],
              ),
            ),
          const PopupMenuDivider(),
          PopupMenuItem<String>(value: '__specific__', child: Text(t.modelSpecific)),
        ],
        child: _ComposerChip(icon: Icons.auto_awesome_outlined, label: modelLabel),
      ),
      if (keys.length > 1)
        PopupMenuButton<String>(
          tooltip: t.selectKey,
          onSelected: onKey,
          itemBuilder: (_) => <PopupMenuEntry<String>>[for (final key in keys) PopupMenuItem<String>(value: key.id, child: Text(key.name))],
          child: _ComposerChip(icon: Icons.key_outlined, label: keyName ?? '—'),
        ),
      Tooltip(
        message: keyFreeOnly ? t.freeModeLocked : (freeOnly ? t.freeModeOnHint : t.freeModeOffHint),
        child: FilterChip(
          avatar: Icon(
            keyFreeOnly ? Icons.lock_outline_rounded : Icons.savings_outlined,
            size: 15,
            color: freeOnly || keyFreeOnly ? palette.accent : palette.textMuted,
          ),
          label: Text(t.freeMode),
          selected: freeOnly || keyFreeOnly,
          // A free-only key cannot leave free mode; the chip stays
          // active and explains that instead of looking disabled.
          onSelected: keyFreeOnly ? (_) => onLockedTap() : onFreeOnly,
          visualDensity: VisualDensity.compact,
          side: BorderSide(color: freeOnly || keyFreeOnly ? palette.accent.withValues(alpha: 0.5) : palette.border),
        ),
      ),
    ];
    if (wrap) return Wrap(crossAxisAlignment: WrapCrossAlignment.center, runSpacing: 6, children: children);
    return Row(mainAxisSize: MainAxisSize.min, children: children);
  }
}

class _ComposerChip extends StatelessWidget {
  const _ComposerChip({required this.icon, required this.label});

  final IconData icon;
  final String label;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Container(
      margin: const EdgeInsets.only(right: 6),
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      decoration: BoxDecoration(color: palette.surfaceRaised, borderRadius: BorderRadius.circular(AppRadius.pill)),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          Icon(icon, size: 14, color: palette.textSoft),
          const SizedBox(width: 6),
          ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 160),
            child: Text(
              label,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(fontSize: 12.5, color: palette.text),
            ),
          ),
          const SizedBox(width: 2),
          Icon(Icons.expand_more_rounded, size: 16, color: palette.textMuted),
        ],
      ),
    );
  }
}

/// The playground's modes in groups, so ten kinds of models stay easy to scan.
const List<(String, List<String>)> _modeGroups = <(String, List<String>)>[
  ('text', <String>['chat', 'embedding', 'rerank', 'moderation']),
  ('create', <String>['image', 'video', 'speech', 'audio']),
  ('listen', <String>['transcription', 'live']),
  ('lookup', <String>['search', 'weather']),
];

String _groupLabel(AppL10n t, String group) => switch (group) {
  'text' => t.pgGroupText,
  'create' => t.pgGroupCreate,
  'listen' => t.pgGroupListen,
  'lookup' => t.pgGroupLookup,
  _ => t.pgGroupOther,
};

// The user's modes per group, in group order; kinds no group names go last.
List<(String, List<CapabilitySummary>)> _grouped(List<CapabilitySummary> modes) {
  final known = _modeGroups.expand((group) => group.$2).toSet();
  return <(String, List<CapabilitySummary>)>[
    for (final (group, ids) in _modeGroups) (group, <CapabilitySummary>[for (final id in ids) ...modes.where((mode) => mode.id == id)]),
    ('other', modes.where((mode) => !known.contains(mode.id)).toList()),
  ].where((group) => group.$2.isNotEmpty).toList();
}

class _ModeRail extends StatelessWidget {
  const _ModeRail({required this.modes, required this.selected, required this.onChanged});

  final List<CapabilitySummary> modes;
  final String selected;
  final ValueChanged<String> onChanged;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return ListView(
      children: <Widget>[
        for (final (group, items) in _grouped(modes)) ...<Widget>[
          Padding(
            padding: const EdgeInsets.fromLTRB(12, 4, 12, 6),
            child: Text(_groupLabel(t, group).toUpperCase(), style: sectionEyebrowStyle(palette)),
          ),
          for (final mode in items) _ModeTile(mode: mode, selected: mode.id == selected, onTap: () => onChanged(mode.id)),
          const SizedBox(height: AppSpacing.md),
        ],
      ],
    );
  }
}

class _ModeTile extends StatelessWidget {
  const _ModeTile({required this.mode, required this.selected, required this.onTap});

  final CapabilitySummary mode;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: 2),
      child: Material(
        color: selected ? palette.accentSoft : Colors.transparent,
        borderRadius: BorderRadius.circular(AppRadius.control),
        child: InkWell(
          borderRadius: BorderRadius.circular(AppRadius.control),
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
            child: Row(
              children: <Widget>[
                Icon(mode.info.icon, size: 18, color: selected ? palette.accent : palette.textSoft),
                const SizedBox(width: 12),
                Expanded(
                  child: Text(
                    mode.info.label(t),
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      color: selected ? palette.text : (mode.total == 0 ? palette.textMuted : palette.textSoft),
                      fontWeight: selected ? FontWeight.w600 : FontWeight.w500,
                    ),
                  ),
                ),
                // Nothing connected yet: a small plus, so long names keep their room.
                if (mode.total == 0) Tooltip(message: t.pgSetUp, child: Icon(Icons.add_circle_outline_rounded, size: 15, color: palette.textMuted)),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// Narrow screens: the current mode as a button that opens the grouped list.
class _ModeButton extends StatelessWidget {
  const _ModeButton({required this.modes, required this.selected, required this.onChanged});

  final List<CapabilitySummary> modes;
  final String selected;
  final ValueChanged<String> onChanged;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final current = capabilityInfo(selected);
    return PopupMenuButton<String>(
      tooltip: t.pgMode,
      onSelected: onChanged,
      itemBuilder: (_) => <PopupMenuEntry<String>>[
        for (final (index, (group, items)) in _grouped(modes).indexed) ...<PopupMenuEntry<String>>[
          if (index > 0) const PopupMenuDivider(),
          PopupMenuItem<String>(enabled: false, height: 32, child: Text(_groupLabel(t, group).toUpperCase(), style: sectionEyebrowStyle(palette))),
          for (final mode in items)
            PopupMenuItem<String>(
              value: mode.id,
              child: Row(
                children: <Widget>[
                  Icon(mode.info.icon, size: 18, color: mode.id == selected ? palette.accent : palette.textSoft),
                  const SizedBox(width: 12),
                  Text(mode.info.label(t)),
                ],
              ),
            ),
        ],
      ],
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 9),
        decoration: BoxDecoration(border: Border.all(color: palette.borderStrong), borderRadius: BorderRadius.circular(AppRadius.control)),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            Icon(current.icon, size: 18, color: palette.accent),
            const SizedBox(width: 10),
            Text(current.label(t), style: TextStyle(fontWeight: FontWeight.w600, color: palette.text)),
            const SizedBox(width: 6),
            Icon(Icons.expand_more_rounded, size: 18, color: palette.textMuted),
          ],
        ),
      ),
    );
  }
}
