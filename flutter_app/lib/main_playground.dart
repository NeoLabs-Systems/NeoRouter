import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:google_fonts/google_fonts.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_routing.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/capabilities.dart';
import 'src/models.dart';

class _ChatMessage {
  _ChatMessage(this.role, this.content, {this.meta, this.error = false});

  final String role;
  final String content;
  final String? meta;
  final bool error;
}

/// Try NeoRouter with one of your keys. One mode per routable capability;
/// today only chat is routable, so the mode switcher stays hidden.
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
      final results = await Future.wait(<Future<Object>>[api.keys(), api.capabilities()]);
      final keys = (results[0] as List<RouterKey>).where((key) => !key.revoked).toList();
      setState(() {
        _keys = keys;
        _keyId = keys.isEmpty ? null : keys.first.id;
        _modes = (results[1] as List<CapabilitySummary>).where((item) => item.routable).toList();
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
      setState(() => _messages.add(_ChatMessage(
            'assistant',
            (message['content'] as String?) ?? '',
            meta: AppL10n.of(context).routedTo(shortModelName('${response['model']}'), formatMs(latency)),
          )));
    } on ApiException catch (error) {
      setState(() => _messages.add(_ChatMessage('assistant', error.message, error: true)));
    } finally {
      if (mounted) setState(() => _sending = false);
      _scrollToEnd();
      _inputFocus.requestFocus();
    }
  }

  Future<void> _pickSpecificModel() async {
    final t = AppL10n.of(context);
    final controller = TextEditingController(text: _model.startsWith('auto') ? '' : _model);
    final value = await showDialog<String>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text(t.modelSpecific),
        content: SizedBox(
          width: 420,
          child: TextField(
            controller: controller,
            autofocus: true,
            decoration: InputDecoration(hintText: t.modelSpecificHint),
            onSubmitted: (value) => Navigator.of(dialogContext).pop(value),
          ),
        ),
        actions: <Widget>[
          TextButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(t.cancel)),
          FilledButton(onPressed: () => Navigator.of(dialogContext).pop(controller.text), child: Text(t.save)),
        ],
      ),
    );
    controller.dispose();
    if (value != null && value.trim().isNotEmpty) setState(() => _model = value.trim());
  }

  Future<void> _editInstructions() async {
    final t = AppL10n.of(context);
    await showDialog<void>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text(t.systemPromptTitle),
        content: SizedBox(
          width: 480,
          child: TextField(controller: _system, autofocus: true, minLines: 4, maxLines: 10, decoration: InputDecoration(hintText: t.systemPromptHint)),
        ),
        actions: <Widget>[FilledButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(t.close))],
      ),
    );
    setState(() {});
  }

  String _modelLabel(AppL10n t) {
    if (_model == 'auto') return presetLabel(t, 'balanced');
    if (_model.startsWith('auto:')) return presetLabel(t, _model.substring(5));
    return shortModelName(_model);
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    if (_loading) return const Center(child: SizedBox.square(dimension: 22, child: CircularProgressIndicator(strokeWidth: 2)));
    final padding = pagePadding(context);
    return Padding(
      padding: EdgeInsets.fromLTRB(padding.left, padding.top, padding.right, 20),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Row(
            children: <Widget>[
              Expanded(child: Text(t.playgroundTitle, style: displayTitleStyle(palette, size: 24))),
              if (_keys.isNotEmpty) ...<Widget>[
                if (_messages.isNotEmpty)
                  TextButton.icon(onPressed: () => setState(_messages.clear), icon: const Icon(Icons.add_comment_outlined, size: 17), label: Text(t.newChat)),
                IconButton(
                  tooltip: t.systemPromptTitle,
                  onPressed: _editInstructions,
                  icon: Icon(Icons.tune_rounded, size: 20, color: _system.text.trim().isEmpty ? null : palette.accent),
                ),
              ],
            ],
          ),
          if (_modes.length > 1) ...<Widget>[
            const SizedBox(height: AppSpacing.sm),
            PageTabs<String>(
              tabs: <({String value, String label, int? count})>[for (final mode in _modes) (value: mode.id, label: mode.info.label(t), count: null)],
              selected: _mode,
              onChanged: (value) => setState(() => _mode = value),
            ),
          ],
          const SizedBox(height: AppSpacing.md),
          Expanded(
            child: _keys.isEmpty
                ? Center(
                    child: EmptyState(
                      icon: Icons.key_outlined,
                      title: t.noKeysTitle,
                      message: t.playgroundNeedsKey,
                      action: FilledButton(onPressed: () => widget.controller.go(AppPage.keys), child: Text(t.createKey)),
                    ),
                  )
                : _chat(context),
          ),
        ],
      ),
    );
  }

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
                        for (final message in _messages) _Bubble(message: message),
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
            _Composer(
              input: _input,
              focus: _inputFocus,
              sending: _sending,
              onSend: _send,
              modelLabel: _modelLabel(t),
              keyName: _keys.where((key) => key.id == _keyId).firstOrNull?.name,
              keys: _keys,
              onModel: (value) => value == '__specific__' ? _pickSpecificModel() : setState(() => _model = value),
              onKey: (value) => setState(() {
                _keyId = value;
                _showLockHint = false;
              }),
              // A free-only key can never leave free mode.
              keyFreeOnly: _keys.where((key) => key.id == _keyId).firstOrNull?.mode == 'free_only',
              freeOnly: _freeOnly,
              onFreeOnly: (value) => setState(() => _freeOnly = value),
              onLockedTap: () => setState(() => _showLockHint = !_showLockHint),
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
              child: Text(t.playgroundEmptyHint, textAlign: TextAlign.center, style: TextStyle(color: palette.textSoft, height: 1.45)),
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
    final text = SelectableText(
      message.content,
      style: TextStyle(color: message.error ? palette.error : palette.text, fontSize: 14.5, height: 1.55),
    );
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
    return Padding(
      padding: const EdgeInsets.only(top: 8, bottom: 14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          text,
          if (message.meta != null) ...<Widget>[
            const SizedBox(height: 8),
            Text(message.meta!, style: TextStyle(color: palette.textMuted, fontSize: 12)),
          ],
        ],
      ),
    );
  }
}

class _Composer extends StatelessWidget {
  const _Composer({
    required this.input,
    required this.focus,
    required this.sending,
    required this.onSend,
    required this.modelLabel,
    required this.keyName,
    required this.keys,
    required this.onModel,
    required this.onKey,
    required this.keyFreeOnly,
    required this.freeOnly,
    required this.onFreeOnly,
    required this.onLockedTap,
  });

  final TextEditingController input;
  final FocusNode focus;
  final bool sending;
  final Future<void> Function([String?]) onSend;
  final String modelLabel;
  final String? keyName;
  final List<RouterKey> keys;
  final ValueChanged<String> onModel;
  final ValueChanged<String> onKey;
  final bool keyFreeOnly;
  final bool freeOnly;
  final ValueChanged<bool> onFreeOnly;
  final VoidCallback onLockedTap;

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
            children: <Widget>[
              PopupMenuButton<String>(
                tooltip: t.modelPicker,
                onSelected: onModel,
                itemBuilder: (_) => <PopupMenuEntry<String>>[
                  for (final id in presetOrder)
                    PopupMenuItem<String>(
                      value: id == 'balanced' ? 'auto' : 'auto:$id',
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
                  avatar: Icon(keyFreeOnly ? Icons.lock_outline_rounded : Icons.savings_outlined, size: 15, color: freeOnly || keyFreeOnly ? palette.accent : palette.textMuted),
                  label: Text(t.freeMode),
                  selected: freeOnly || keyFreeOnly,
                  // A free-only key cannot leave free mode; the chip stays
                  // active and explains that instead of looking disabled.
                  onSelected: keyFreeOnly ? (_) => onLockedTap() : onFreeOnly,
                  visualDensity: VisualDensity.compact,
                  side: BorderSide(color: freeOnly || keyFreeOnly ? palette.accent.withValues(alpha: 0.5) : palette.border),
                ),
              ),
              // The keyboard hint gives way first when the row gets narrow.
              Expanded(
                child: Text(
                  t.enterToSend,
                  maxLines: 1,
                  overflow: TextOverflow.fade,
                  softWrap: false,
                  textAlign: TextAlign.end,
                  style: GoogleFonts.geist(fontSize: 11.5, color: palette.textMuted),
                ),
              ),
              const SizedBox(width: 10),
              IconButton.filled(
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
            constraints: const BoxConstraints(maxWidth: 180),
            child: Text(label, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 12.5, color: palette.text)),
          ),
          const SizedBox(width: 2),
          Icon(Icons.expand_more_rounded, size: 16, color: palette.textMuted),
        ],
      ),
    );
  }
}
