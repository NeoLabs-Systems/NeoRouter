import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';

class _CopyName extends StatelessWidget {
  const _CopyName({required this.value});
  final String value;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return Tooltip(
      message: t.copy,
      child: InkWell(
        borderRadius: BorderRadius.circular(AppRadius.tag),
        onTap: () {
          Clipboard.setData(ClipboardData(text: value));
          showSnack(context, t.copied);
        },
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
          decoration: BoxDecoration(
            color: palette.surfaceRaised,
            borderRadius: BorderRadius.circular(AppRadius.tag),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              Flexible(
                child: Text(
                  value,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    fontFamily: 'monospace',
                    fontSize: 12.5,
                  ),
                ),
              ),
              const SizedBox(width: 6),
              Icon(Icons.copy_rounded, size: 14, color: palette.textSoft),
            ],
          ),
        ),
      ),
    );
  }
}

class RoutingGroupSetup extends StatefulWidget {
  const RoutingGroupSetup({
    super.key,
    required this.controller,
    required this.name,
    this.subagent,
    this.appSetupBuilder,
  });
  final NeoRouterController controller;
  final String name;
  final String? subagent;
  final WidgetBuilder? appSetupBuilder;

  @override
  State<RoutingGroupSetup> createState() => _RoutingGroupSetupState();
}

class _RoutingGroupSetupState extends State<RoutingGroupSetup> {
  bool _child = false;
  bool _checking = false;
  String? _result;
  String? _error;

  Future<void> _check() async {
    final t = AppL10n.of(context);
    setState(() {
      _checking = true;
      _result = null;
      _error = null;
    });
    try {
      final preview = await widget.controller.api.preview({
        'model': 'pool:${widget.name}',
        if (_child) 'workload': 'subagent',
      });
      if (!mounted) return;
      final selected = preview['selection'] as Map?;
      setState(
        () => _result = selected == null
            ? t.routingGroupPreviewUnavailable
            : t.routingGroupPreviewModel(selected['model'] as String),
      );
    } catch (error) {
      if (mounted) {
        setState(
          () => _error = error is ApiException ? error.message : '$error',
        );
      }
    } finally {
      if (mounted) setState(() => _checking = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    return AlertDialog(
      title: Text(t.routingGroupUseInApp),
      scrollable: true,
      content: SizedBox(
        width: 420,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(t.routingGroupAppModel),
            const SizedBox(height: AppSpacing.xs),
            _CopyName(value: 'pool:${widget.name}'),
            if (widget.appSetupBuilder case final builder?)
              TextButton.icon(
                onPressed: () => Navigator.of(context).push(
                  MaterialPageRoute<void>(
                    builder: (context) => Scaffold(
                      appBar: AppBar(title: Text(t.connectApps)),
                      body: SafeArea(
                        child: SingleChildScrollView(
                          padding: const EdgeInsets.all(AppSpacing.lg),
                          child: Center(
                            child: ConstrainedBox(
                              constraints: const BoxConstraints(maxWidth: 960),
                              child: Builder(builder: builder),
                            ),
                          ),
                        ),
                      ),
                    ),
                  ),
                ),
                icon: const Icon(Icons.arrow_forward_rounded, size: 16),
                label: Text(t.connectApps),
              ),
            if (widget.subagent case final String child) ...[
              const SizedBox(height: AppSpacing.md),
              Text(t.routingGroupAppSubagent),
              const SizedBox(height: AppSpacing.xs),
              _CopyName(value: 'pool:$child'),
              const SizedBox(height: AppSpacing.sm),
              SelectableText(t.routingGroupAppSubagentHeader),
              CheckboxListTile(
                contentPadding: EdgeInsets.zero,
                title: Text(t.routingGroupPreviewSubagent),
                value: _child,
                onChanged: _checking
                    ? null
                    : (value) => setState(() {
                        _child = value == true;
                        _result = null;
                        _error = null;
                      }),
              ),
            ],
            const SizedBox(height: AppSpacing.md),
            Text(t.routingGroupPreviewHint),
            if (_checking) const LinearProgressIndicator(),
            if (_result != null)
              Semantics(liveRegion: true, child: Text(_result!)),
            if (_error != null) InlineError(message: _error!),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: Text(t.cancel),
        ),
        FilledButton(
          onPressed: _checking ? null : _check,
          child: Text(t.routingGroupPreview),
        ),
      ],
    );
  }
}
