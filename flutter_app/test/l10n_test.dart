import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

void main() {
  test('German and English ARB files define the same keys', () {
    Map<String, dynamic> read(String locale) =>
        jsonDecode(File('lib/l10n/app_$locale.arb').readAsStringSync()) as Map<String, dynamic>;
    Set<String> keys(Map<String, dynamic> arb) => arb.keys.where((key) => !key.startsWith('@')).toSet();
    expect(keys(read('de')), keys(read('en')));
  });

  test('ARB files match their source, scripts/l10n_strings.py', () {
    final result = Process.runSync('python3', <String>['../scripts/l10n_strings.py', '--check']);
    expect(result.exitCode, 0, reason: '${result.stderr}');
  });
}
