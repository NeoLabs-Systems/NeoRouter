import 'package:flutter/material.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/models.dart';

class _ChatMessage {
  _ChatMessage(this.role, this.content, {this.meta, this.error = false});

  final String role;
  final String content;
  final String? meta;
  final bool error;
}

class PlaygroundScreen extends StatefulWidget {
  const PlaygroundScreen({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<PlaygroundScreen> createState() => _PlaygroundScreenState();
}

class _PlaygroundScreenState extends State<PlaygroundScreen> {
  List<RouterKey> _keys = <RouterKey>[];
  String? _keyId;
  final TextEditingController _model = TextEditingController(text: 'auto');
  final TextEditingController _system = TextEditingController();
  final TextEditingController _input = TextEditingController();
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
    _model.dispose();
    _system.dispose();
    _input.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final keys = (await widget.controller.api.keys())
          .where((key) => !key.revoked)
          .toList();
      setState(() {
        _keys = keys;
        _keyId = keys.isEmpty ? null : keys.first.id;
      });
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _send() async {
    final text = _input.text.trim();
    if (text.isEmpty || _keyId == null || _sending) return;
    setState(() {
      _messages.add(_ChatMessage('user', text));
      _input.clear();
      _sending = true;
    });
    final history = <Map<String, String>>[
      if (_system.text.trim().isNotEmpty)
        <String, String>{'role': 'system', 'content': _system.text.trim()},
      for (final message in _messages.where((item) => !item.error))
        <String, String>{'role': message.role, 'content': message.content},
    ];
    final started = DateTime.now();
    try {
      final response = await widget.controller.api.playground(<String, Object?>{
        'keyId': _keyId,
        'model': _model.text.trim(),
        'messages': history,
      });
      final message = (response['choices'] as List).first['message'] as Map;
      final content = (message['content'] as String?) ?? '';
      final latency = DateTime.now().difference(started).inMilliseconds;
      if (!mounted) return;
      setState(
        () => _messages.add(
          _ChatMessage(
            'assistant',
            content,
            meta: AppL10n.of(
              context,
            ).routedTo('${response['model']}', formatMs(latency)),
          ),
        ),
      );
    } on ApiException catch (error) {
      setState(
        () => _messages.add(
          _ChatMessage('assistant', error.message, error: true),
        ),
      );
    } finally {
      if (mounted) setState(() => _sending = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    if (_loading) return const Center(child: CircularProgressIndicator());
    return Padding(
      padding: pagePadding(context),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          ScreenHeader(
            title: t.playgroundTitle,
            description: t.playgroundDescription,
            trailing: OutlinedButton(
              onPressed: () => setState(_messages.clear),
              child: Text(t.clearChat),
            ),
          ),
          if (_keys.isEmpty)
            SectionCard(
              child: EmptyState(
                icon: Icons.key_outlined,
                title: t.noKeysTitle,
                message: t.playgroundNeedsKey,
              ),
            )
          else ...<Widget>[
            Wrap(
              spacing: AppSpacing.sm,
              runSpacing: AppSpacing.sm,
              children: <Widget>[
                SizedBox(
                  width: 260,
                  child: DropdownButtonFormField<String>(
                    initialValue: _keyId,
                    decoration: InputDecoration(
                      labelText: t.selectKey,
                      isDense: true,
                    ),
                    items: <DropdownMenuItem<String>>[
                      for (final key in _keys)
                        DropdownMenuItem<String>(
                          value: key.id,
                          child: Text(key.name),
                        ),
                    ],
                    onChanged: (value) => setState(() => _keyId = value),
                  ),
                ),
                SizedBox(
                  width: 320,
                  child: TextField(
                    controller: _model,
                    decoration: InputDecoration(
                      labelText: t.modelField,
                      isDense: true,
                    ),
                  ),
                ),
                SizedBox(
                  width: 380,
                  child: TextField(
                    controller: _system,
                    decoration: InputDecoration(
                      labelText: t.systemPrompt,
                      isDense: true,
                    ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: AppSpacing.md),
            Expanded(
              child: Container(
                padding: const EdgeInsets.all(AppSpacing.md),
                decoration: BoxDecoration(
                  color: palette.surface.withValues(alpha: 0.72),
                  borderRadius: BorderRadius.circular(20),
                  border: Border.all(color: palette.borderStrong),
                ),
                child: ListView(
                  children: <Widget>[
                    for (final message in _messages)
                      Align(
                        alignment: message.role == 'user'
                            ? Alignment.centerRight
                            : Alignment.centerLeft,
                        child: Container(
                          constraints: const BoxConstraints(maxWidth: 720),
                          margin: const EdgeInsets.only(bottom: AppSpacing.sm),
                          padding: const EdgeInsets.all(AppSpacing.sm),
                          decoration: BoxDecoration(
                            color: message.error
                                ? palette.error.withValues(alpha: 0.1)
                                : message.role == 'user'
                                ? palette.accentSoft
                                : palette.surfaceMuted.withValues(alpha: 0.7),
                            borderRadius: BorderRadius.circular(AppRadius.card),
                          ),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: <Widget>[
                              SelectableText(
                                message.content,
                                style: TextStyle(
                                  color: message.error
                                      ? palette.error
                                      : palette.text,
                                  height: 1.45,
                                ),
                              ),
                              if (message.meta != null) ...<Widget>[
                                const SizedBox(height: 4),
                                Text(
                                  message.meta!,
                                  style: TextStyle(
                                    color: palette.textMuted,
                                    fontSize: 12,
                                    fontFamily: 'monospace',
                                  ),
                                ),
                              ],
                            ],
                          ),
                        ),
                      ),
                    if (_sending)
                      const Padding(
                        padding: EdgeInsets.all(8),
                        child: LinearProgressIndicator(),
                      ),
                  ],
                ),
              ),
            ),
            const SizedBox(height: AppSpacing.sm),
            Row(
              children: <Widget>[
                Expanded(
                  child: TextField(
                    controller: _input,
                    minLines: 1,
                    maxLines: 5,
                    onSubmitted: (_) => _send(),
                    decoration: InputDecoration(hintText: t.messageHint),
                  ),
                ),
                const SizedBox(width: AppSpacing.sm),
                FilledButton.icon(
                  onPressed: _sending ? null : _send,
                  icon: const Icon(Icons.send_rounded, size: 18),
                  label: Text(t.send),
                ),
              ],
            ),
          ],
        ],
      ),
    );
  }
}
