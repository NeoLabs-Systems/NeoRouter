import 'dart:convert';
import 'package:flutter/material.dart';
import 'src/web_media.dart';
import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';

class AdminProxies extends StatefulWidget {
  const AdminProxies({super.key, required this.controller});
  final NeoRouterController controller;
  @override
  State<AdminProxies> createState() => _AdminProxiesState();
}

class _AdminProxiesState extends State<AdminProxies> {
  final _text = TextEditingController();
  final _testUrl = TextEditingController();
  List<Map> _rows = [];
  bool _enabled = false, _direct = true, _busy = false, _loading = true;
  String _protocol = 'socks5';
  String? _error, _result;
  String _search = '';
  int _page = 0;
  static const _pageSize = 25;
  static const _base = '/api/v1/admin/proxies';
  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _text.dispose();
    _testUrl.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final data = await widget.controller.api.client.get(_base);
      if (mounted) {
        setState(() {
          _rows = (data['proxies'] as List).cast<Map>();
          _enabled = (data['policy'] as Map)['enabled'] == true;
          _direct = (data['policy'] as Map)['fallbackDirect'] == true;
          _loading = false;
          _error = null;
        });
      }
    } catch (error) {
      if (mounted) {
        setState(() {
          _error = '$error';
          _loading = false;
        });
      }
    }
  }

  Future<void> _run(Future<void> Function() action) async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await action();
      await _load();
    } catch (error) {
      if (mounted) setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _policy(bool enabled, bool direct) => _run(
    () => widget.controller.api.client.put('$_base/policy', {
      'enabled': enabled,
      'fallbackDirect': direct,
    }),
  );
  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final filtered = _rows
        .where(
          (row) => '${row['endpoint']}'.toLowerCase().contains(
            _search.toLowerCase(),
          ),
        )
        .toList();
    final pages = (filtered.length / _pageSize).ceil().clamp(1, 1000000);
    final page = _page.clamp(0, pages - 1);
    final visible = filtered.skip(page * _pageSize).take(_pageSize);
    return AsyncBody(
      loading: _loading,
      error: _loading ? _error : null,
      onRetry: _load,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          SettingsHeading(title: t.proxyTitle, description: t.proxyHint),
          SettingsCardList(
            children: [
              SwitchListTile(
                title: Text(t.proxyEnable),
                value: _enabled,
                onChanged: _busy ? null : (value) => _policy(value, _direct),
              ),
              SwitchListTile(
                title: Text(t.proxyDirect),
                subtitle: Text(t.proxyDirectHint),
                value: _direct,
                onChanged: _busy ? null : (value) => _policy(_enabled, value),
              ),
            ],
          ),
          const SizedBox(height: 24),
          SettingsHeading(title: t.proxyImport),
          Text(t.proxyFormats),
          const SizedBox(height: 8),
          DropdownButtonFormField<String>(
            initialValue: _protocol,
            decoration: InputDecoration(labelText: t.proxyProtocol),
            items: [
              for (final value in ['socks5', 'http', 'https'])
                DropdownMenuItem(
                  value: value,
                  child: Text(value.toUpperCase()),
                ),
            ],
            onChanged: _busy
                ? null
                : (value) => setState(() => _protocol = value!),
          ),
          TextField(
            controller: _text,
            enabled: !_busy,
            minLines: 4,
            maxLines: 10,
            autocorrect: false,
            enableSuggestions: false,
            decoration: InputDecoration(labelText: t.proxyPaste),
          ),
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton.icon(
              icon: const Icon(Icons.upload_file),
              label: Text(t.proxyChooseFile),
              onPressed: _busy
                  ? null
                  : () async {
                      try {
                        final file = await pickFile(
                          accept: '.txt,.csv,.tsv,text/plain,text/csv',
                          maxBytes: 1000000,
                        );
                        if (file == null || !mounted) return;
                        setState(
                          () => _text.text = utf8
                              .decode(file.bytes)
                              .replaceFirst('\uFEFF', ''),
                        );
                      } catch (_) {
                        if (mounted) setState(() => _error = t.proxyFileError);
                      }
                    },
            ),
          ),
          Align(
            alignment: Alignment.centerLeft,
            child: FilledButton.icon(
              icon: const Icon(Icons.playlist_add),
              label: Text(t.proxyImport),
              onPressed: _busy
                  ? null
                  : () => _run(() async {
                      final lines = _text.text.split(RegExp(r'\r?\n'));
                      final data = await widget.controller.api.client.post(
                        '$_base/import',
                        {'text': _text.text, 'protocol': _protocol},
                      );
                      if (!mounted) return;
                      setState(() {
                        _result = t.proxyImportResult(
                          '${data['added']}',
                          '${data['duplicates']}',
                          '${(data['invalidLines'] as List).length}',
                        );
                        _text.text = (data['invalidLines'] as List)
                            .map((number) => lines[(number as int) - 1])
                            .join('\n');
                      });
                    }),
            ),
          ),
          if (_result != null)
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 8),
              child: Text(_result!),
            ),
          if (_error != null) InlineError(message: _error!),
          const SizedBox(height: 24),
          Row(
            children: [
              Expanded(child: SettingsHeading(title: t.proxyList)),
              IconButton(
                tooltip: t.refresh,
                onPressed: _busy ? null : () => _run(() async {}),
                icon: const Icon(Icons.refresh),
              ),
            ],
          ),
          TextField(
            controller: _testUrl,
            decoration: InputDecoration(
              labelText: t.proxyTestUrl,
              helperText: t.proxyTestHint,
            ),
            onChanged: (_) => setState(() {}),
          ),
          const SizedBox(height: 12),
          TextField(
            decoration: InputDecoration(
              labelText: t.search,
              prefixIcon: const Icon(Icons.search),
            ),
            onChanged: (value) => setState(() {
              _search = value;
              _page = 0;
            }),
          ),
          const SizedBox(height: 12),
          if (_rows.isEmpty) Text(t.proxyEmpty),
          for (final row in visible)
            Padding(
              padding: const EdgeInsets.only(bottom: 8),
              child: AppCard(
                padding: const EdgeInsets.all(16),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Text('${row['endpoint']}', softWrap: true),
                    Text(
                      (row['until'] as num) >
                              DateTime.now().millisecondsSinceEpoch
                          ? t.proxyCooling
                          : row['latencyMs'] == null
                          ? t.proxyUnmeasured
                          : t.proxyLatency(
                              '${(row['latencyMs'] as num).round()}',
                            ),
                    ),
                    Text(
                      t.proxyActivity('${row['active']}', '${row['served']}'),
                    ),
                    Wrap(
                      spacing: 8,
                      crossAxisAlignment: WrapCrossAlignment.center,
                      children: [
                        Text(row['enabled'] == true ? t.enabled : t.disabled),
                        Switch(
                          value: row['enabled'] == true,
                          onChanged: _busy
                              ? null
                              : (value) => _run(() async {
                                  await widget.controller.api.client.patch(
                                    '$_base/${row['id']}',
                                    {'enabled': value},
                                  );
                                }),
                        ),
                        TextButton(
                          onPressed: _busy || _testUrl.text.trim().isEmpty
                              ? null
                              : () => _run(() async {
                                  final result = await widget
                                      .controller
                                      .api
                                      .client
                                      .post('$_base/${row['id']}/test', {
                                        'url': _testUrl.text.trim(),
                                      });
                                  if (mounted) {
                                    setState(
                                      () => _result = result['ok'] == true
                                          ? t.proxyTestOk
                                          : t.proxyTestFailed,
                                    );
                                  }
                                }),
                          child: Text(t.proxyTest),
                        ),
                        TextButton(
                          onPressed: _busy
                              ? null
                              : () async {
                                  final confirmed = await confirmAction(
                                    context,
                                    title: t.delete,
                                    message: t.proxyDeleteConfirm(
                                      '${row['endpoint']}',
                                    ),
                                    confirmLabel: t.delete,
                                    cancelLabel: t.cancel,
                                  );
                                  if (confirmed && mounted) {
                                    await _run(
                                      () => widget.controller.api.client.delete(
                                        '$_base/${row['id']}',
                                      ),
                                    );
                                  }
                                },
                          child: Text(t.delete),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
            ),
          if (filtered.isNotEmpty)
            Row(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                IconButton(
                  tooltip: t.proxyPrevious,
                  onPressed: page == 0
                      ? null
                      : () => setState(() => _page = page - 1),
                  icon: const Icon(Icons.chevron_left),
                ),
                Text(
                  t.proxyPage('${page + 1}', '$pages', '${filtered.length}'),
                ),
                IconButton(
                  tooltip: t.proxyNext,
                  onPressed: page + 1 >= pages
                      ? null
                      : () => setState(() => _page = page + 1),
                  icon: const Icon(Icons.chevron_right),
                ),
              ],
            ),
          if (_busy) const LinearProgressIndicator(),
        ],
      ),
    );
  }
}
