// scripts/apply_env.dart
//
// Reads .env (KEY=VALUE pairs) and generates lib/config/env_config.dart
// with typed Dart constants. Run this before building the app whenever
// you change .env.
//
// Usage:
//   dart scripts/apply_env.dart
//   dart scripts/apply_env.dart --env path/to/custom.env

import 'dart:io';

void main(List<String> args) {
  // Allow overriding the .env path via --env flag
  String envPath = '.env';
  for (int i = 0; i < args.length - 1; i++) {
    if (args[i] == '--env') envPath = args[i + 1];
  }

  final envFile = File(envPath);
  if (!envFile.existsSync()) {
    stderr.writeln('ERROR: $envPath not found.');
    stderr.writeln('       Copy .env.example to .env and edit it, then re-run.');
    exit(1);
  }

  final lines = envFile.readAsLinesSync();
  final entries = <String, String>{};

  for (final raw in lines) {
    final line = raw.trim();
    if (line.isEmpty || line.startsWith('#')) continue;
    final eq = line.indexOf('=');
    if (eq < 1) continue;
    final key = line.substring(0, eq).trim();
    final val = line.substring(eq + 1).trim();
    if (key.isEmpty) continue;
    entries[key] = val;
  }

  if (entries.isEmpty) {
    stderr.writeln('ERROR: $envPath contains no valid KEY=VALUE entries.');
    exit(1);
  }

  final buf = StringBuffer();
  buf.writeln('// GENERATED FILE — DO NOT EDIT MANUALLY');
  buf.writeln('// Source: .env');
  buf.writeln('// Regenerate: dart scripts/apply_env.dart');
  buf.writeln('//');
  buf.writeln('// This file is committed with default values so the app');
  buf.writeln('// builds without a local .env file (e.g. CI, new contributors).');
  buf.writeln('');
  buf.writeln('// ignore_for_file: constant_identifier_names');
  buf.writeln('abstract final class EnvConfig {');

  for (final kv in entries.entries) {
    final key = kv.key;
    final val = kv.value;

    if (val == 'true' || val == 'false') {
      buf.writeln('  static const bool $key = $val;');
    } else if (int.tryParse(val) != null) {
      buf.writeln('  static const int $key = $val;');
    } else {
      final escaped = val.replaceAll(r'\', r'\\').replaceAll("'", r"\'");
      buf.writeln("  static const String $key = '$escaped';");
    }
  }

  buf.writeln('}');

  const outPath = 'lib/config/env_config.dart';
  Directory('lib/config').createSync(recursive: true);
  File(outPath).writeAsStringSync(buf.toString());

  stdout.writeln('Generated $outPath from $envPath (${entries.length} keys)');
}
