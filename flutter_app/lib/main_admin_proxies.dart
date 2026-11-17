import 'dart:convert';
import 'package:flutter/material.dart';
import 'src/web_media.dart';
import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_theme.dart';

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
  final Set<String> _picked = {};
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
          _picked.retainAll(_rows.map((row) => '${row['id']}'));
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
  Future<void> _import(AppL10n t) => _run(() async {
    final lines = _text.text.split(RegExp(r'\r?\n'));
    final data = await widget.controller.api.client.post('$_base/import', {
      'text': _text.text,
      'protocol': _protocol,
    });
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
  });

  Future<void> _pickFile(AppL10n t) async {
    try {
      final file = await pickFile(
        accept: '.txt,.csv,.tsv,text/plain,text/csv',
        maxBytes: 1000000,
      );
      if (file == null || !mounted) return;
      setState(
        () => _text.text = utf8.decode(file.bytes).replaceFirst('\uFEFF', ''),
      );
    } catch (_) {
      if (mounted) setState(() => _error = t.proxyFileError);
    }
  }

  Future<void> _test(AppL10n t, Map row) => _run(() async {
    final result = await widget.controller.api.client.post(
      '$_base/${row['id']}/test',
      {'url': _testUrl.text.trim()},
    );
    if (mounted) {
      setState(
        () =>
            _result = result['ok'] == true ? t.proxyTestOk : t.proxyTestFailed,
      );
    }
  });

  Future<void> _delete(AppL10n t, Map row) async {
    final confirmed = await confirmAction(
      context,
      title: t.delete,
      message: t.proxyDeleteConfirm('${row['endpoint']}'),
      confirmLabel: t.delete,
      cancelLabel: t.cancel,
    );
    if (confirmed && mounted) {
      await _run(
        () => widget.controller.api.client.delete('$_base/${row['id']}'),
      );
    }
  }

  // Selected ids, or every proxy matching the search when all are picked.
  Future<void> _bulk(AppL10n t, String action, List<Map> filtered) async {
    final ids = _picked.toList();
    if (action == 'delete') {
      final confirmed = await confirmAction(
        context,
        title: t.delete,
        message: t.proxyDeleteSelected('${ids.length}'),
        confirmLabel: t.delete,
        cancelLabel: t.cancel,
      );
      if (!confirmed || !mounted) return;
    }
    final everything = ids.length == _rows.length;
    await _run(() async {
      await widget.controller.api.client.post('$_base/bulk', {
        'action': action,
        if (everything) 'all': true else 'ids': ids,
      });
      if (mounted) setState(_picked.clear);
    });
  }

  String _state(Map row) {
    if (row['enabled'] != true) return 'off';
    return (row['until'] as num) > DateTime.now().millisecondsSinceEpoch
        ? 'resting'
        : 'ready';
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
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
    final counts = {'ready': 0, 'resting': 0, 'off': 0};
    for (final row in _rows) {
      counts[_state(row)] = counts[_state(row)]! + 1;
    }
    final stateColor = {
      'ready': palette.accent,
      'resting': palette.warning,
      'off': palette.textMuted.withValues(alpha: .4),
    };
    final stateLabel = {
      'ready': t.proxyHealthy,
      'resting': t.proxyResting,
      'off': t.proxyOff,
    };
    final fastest =
        _rows
            .where((row) => row['latencyMs'] is num && _state(row) == 'ready')
            .toList()
          ..sort(
            (a, b) => (a['latencyMs'] as num).compareTo(b['latencyMs'] as num),
          );
    final top = fastest.take(12).toList();
    final slowest = top.isEmpty
        ? 1.0
        : (top.last['latencyMs'] as num).toDouble();
    return AsyncBody(
      loading: _loading,
      error: _loading ? _error : null,
      onRetry: _load,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Expanded(child: SettingsHeading(title: t.proxyTitle)),
              Tooltip(
                message: t.proxyHint,
                child: Icon(
                  Icons.info_outline,
                  size: 18,
                  color: palette.textMuted,
                ),
              ),
              IconButton(
                tooltip: t.refresh,
                onPressed: _busy ? null : () => _run(() async {}),
                icon: const Icon(Icons.refresh),
              ),
            ],
          ),
          SettingsCardList(
            children: [
              SwitchListTile(
                dense: true,
                title: Text(t.proxyEnable),
                value: _enabled,
                onChanged: _busy ? null : (value) => _policy(value, _direct),
              ),
              SwitchListTile(
                dense: true,
                title: Text(t.proxyDirect),
                subtitle: Text(t.proxyDirectHint),
                value: _direct,
                onChanged: _busy ? null : (value) => _policy(_enabled, value),
              ),
            ],
          ),
          if (_rows.isNotEmpty) ...[
            const SizedBox(height: 16),
            AppCard(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  // Share of proxies per state, as one stacked bar.
                  ClipRRect(
                    borderRadius: BorderRadius.circular(99),
                    child: SizedBox(
                      height: 10,
                      child: Row(
                        children: [
                          for (final key in counts.keys)
                            if (counts[key]! > 0)
                              Expanded(
                                flex: counts[key]!,
                                child: Container(color: stateColor[key]),
                              ),
                        ],
                      ),
                    ),
                  ),
                  const SizedBox(height: 10),
                  Wrap(
                    spacing: 16,
                    children: [
                      for (final key in counts.keys)
                        Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            Icon(Icons.circle, size: 8, color: stateColor[key]),
                            const SizedBox(width: 6),
                            Text(
                              '${stateLabel[key]} ${counts[key]}',
                              style: TextStyle(
                                color: palette.textSoft,
                                fontSize: 12,
                              ),
                            ),
                          ],
                        ),
                    ],
                  ),
                  if (top.isNotEmpty) ...[
                    const SizedBox(height: 18),
                    Text(
                      t.proxyLatencyChart,
                      style: TextStyle(color: palette.textMuted, fontSize: 12),
                    ),
                    const SizedBox(height: 8),
                    for (final row in top)
                      Padding(
                        padding: const EdgeInsets.symmetric(vertical: 2),
                        child: Row(
                          children: [
                            SizedBox(
                              width: 170,
                              child: Text(
                                '${row['endpoint']}',
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: TextStyle(
                                  color: palette.textSoft,
                                  fontSize: 11,
                                ),
                              ),
                            ),
                            Expanded(
                              child: Align(
                                alignment: Alignment.centerLeft,
                                child: FractionallySizedBox(
                                  widthFactor:
                                      ((row['latencyMs'] as num) / slowest)
                                          .clamp(.04, 1)
                                          .toDouble(),
                                  child: Container(
                                    height: 8,
                                    decoration: BoxDecoration(
                                      color: palette.accent.withValues(
                                        alpha: .75,
                                      ),
                                      borderRadius: BorderRadius.circular(99),
                                    ),
                                  ),
                                ),
                              ),
                            ),
                            SizedBox(
                              width: 60,
                              child: Text(
                                formatMs(row['latencyMs'] as num),
                                textAlign: TextAlign.right,
                                style: TextStyle(
                                  color: palette.textMuted,
                                  fontSize: 11,
                                ),
                              ),
                            ),
                          ],
                        ),
                      ),
                  ],
                ],
              ),
            ),
          ],
          const SizedBox(height: 16),
          AppCard(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: ExpansionTile(
              tilePadding: EdgeInsets.zero,
              initiallyExpanded: _rows.isEmpty,
              leading: const Icon(Icons.playlist_add),
              title: Text(t.proxyImport),
              childrenPadding: const EdgeInsets.only(bottom: 16),
              children: [
                Row(
                  children: [
                    SizedBox(
                      width: 140,
                      child: DropdownButtonFormField<String>(
                        initialValue: _protocol,
                        isDense: true,
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
                    ),
                    const Spacer(),
                    TextButton.icon(
                      icon: const Icon(Icons.upload_file, size: 18),
                      label: Text(t.proxyChooseFile),
                      onPressed: _busy ? null : () => _pickFile(t),
                    ),
                  ],
                ),
                const SizedBox(height: 8),
                TextField(
                  controller: _text,
                  enabled: !_busy,
                  minLines: 3,
                  maxLines: 8,
                  autocorrect: false,
                  enableSuggestions: false,
                  decoration: InputDecoration(
                    hintText: t.proxyPaste,
                    helperText: t.proxyFormats,
                  ),
                ),
                const SizedBox(height: 8),
                Align(
                  alignment: Alignment.centerRight,
                  child: FilledButton(
                    onPressed: _busy ? null : () => _import(t),
                    child: Text(t.proxyImport),
                  ),
                ),
              ],
            ),
          ),
          if (_result != null)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: InlineSuccess(message: _result!),
            ),
          if (_error != null) InlineError(message: _error!),
          const SizedBox(height: 16),
          Wrap(
            spacing: 10,
            runSpacing: 10,
            children: [
              SizedBox(
                width: 240,
                child: TextField(
                  decoration: InputDecoration(
                    isDense: true,
                    hintText: t.search,
                    prefixIcon: const Icon(Icons.search, size: 18),
                  ),
                  onChanged: (value) => setState(() {
                    _search = value;
                    _page = 0;
                  }),
                ),
              ),
              SizedBox(
                width: 300,
                child: Tooltip(
                  message: t.proxyTestHint,
                  child: TextField(
                    controller: _testUrl,
                    decoration: InputDecoration(
                      isDense: true,
                      hintText: t.proxyTestUrl,
                      prefixIcon: const Icon(Icons.network_check, size: 18),
                    ),
                    onChanged: (_) => setState(() {}),
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 10),
          if (_rows.isNotEmpty)
            Row(
              children: [
                Checkbox(
                  tristate: true,
                  value: _picked.isEmpty
                      ? false
                      : filtered.every(
                          (row) => _picked.contains('${row['id']}'),
                        )
                      ? true
                      : null,
                  onChanged: _busy
                      ? null
                      : (_) => setState(() {
                          final all = filtered.every(
                            (row) => _picked.contains('${row['id']}'),
                          );
                          if (all) {
                            _picked.clear();
                          } else {
                            _picked.addAll(
                              filtered.map((row) => '${row['id']}'),
                            );
                          }
                        }),
                ),
                Text(
                  _picked.isEmpty
                      ? t.proxySelectAll
                      : t.proxySelected('${_picked.length}'),
                  style: TextStyle(color: palette.textSoft, fontSize: 12),
                ),
                const Spacer(),
                if (_picked.isNotEmpty) ...[
                  TextButton(
                    onPressed: _busy
                        ? null
                        : () => _bulk(t, 'enable', filtered),
                    child: Text(t.proxyEnableAction),
                  ),
                  TextButton(
                    onPressed: _busy
                        ? null
                        : () => _bulk(t, 'disable', filtered),
                    child: Text(t.proxyDisableAction),
                  ),
                  TextButton(
                    style: TextButton.styleFrom(foregroundColor: palette.error),
                    onPressed: _busy
                        ? null
                        : () => _bulk(t, 'delete', filtered),
                    child: Text(t.delete),
                  ),
                ],
              ],
            ),
          if (_rows.isEmpty)
            Text(t.proxyEmpty, style: TextStyle(color: palette.textMuted))
          else
            SettingsCardList(
              children: [
                for (final row in visible)
                  Padding(
                    padding: const EdgeInsets.only(right: 4),
                    child: Row(
                      children: [
                        Checkbox(
                          value: _picked.contains('${row['id']}'),
                          onChanged: _busy
                              ? null
                              : (value) => setState(
                                  () => value == true
                                      ? _picked.add('${row['id']}')
                                      : _picked.remove('${row['id']}'),
                                ),
                        ),
                        Icon(
                          Icons.circle,
                          size: 8,
                          color: stateColor[_state(row)],
                        ),
                        const SizedBox(width: 10),
                        Expanded(
                          child: Text(
                            '${row['endpoint']}',
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: TextStyle(color: palette.text, fontSize: 13),
                          ),
                        ),
                        Tooltip(
                          message: t.proxyActivity(
                            '${row['active']}',
                            '${row['served']}',
                          ),
                          child: Text(
                            row['latencyMs'] == null
                                ? '—'
                                : formatMs(row['latencyMs'] as num),
                            style: TextStyle(
                              color: palette.textMuted,
                              fontSize: 12,
                            ),
                          ),
                        ),
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
                        PopupMenuButton<String>(
                          enabled: !_busy,
                          iconSize: 18,
                          onSelected: (action) => action == 'test'
                              ? _test(t, row)
                              : _delete(t, row),
                          itemBuilder: (_) => [
                            PopupMenuItem(
                              value: 'test',
                              enabled: _testUrl.text.trim().isNotEmpty,
                              child: Text(t.proxyTest),
                            ),
                            PopupMenuItem(
                              value: 'delete',
                              child: Text(t.delete),
                            ),
                          ],
                        ),
                      ],
                    ),
                  ),
              ],
            ),
          if (pages > 1)
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
                  style: TextStyle(color: palette.textMuted, fontSize: 12),
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
          if (_busy) const LinearProgressIndicator(minHeight: 2),
        ],
      ),
    );
  }
}
