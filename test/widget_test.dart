import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:natproxy/main.dart';

void main() {
  testWidgets('Home screen shows role selection buttons', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(const MyApp());

    expect(find.text('P2P Internet Sharing'), findsOneWidget);
    expect(find.text('Share Internet (Server)'), findsOneWidget);
    expect(find.text('Connect (Client)'), findsOneWidget);
  });

  testWidgets('Navigate to server screen', (WidgetTester tester) async {
    await tester.pumpWidget(const MyApp());

    await tester.tap(find.text('Share Internet (Server)'));
    await tester.pumpAndSettle();

    expect(find.text('Share Internet'), findsOneWidget);
    expect(find.text('Server Stopped'), findsOneWidget);
    expect(find.text('Start Sharing'), findsOneWidget);
  });

  testWidgets('Navigate to client screen', (WidgetTester tester) async {
    await tester.pumpWidget(const MyApp());

    await tester.tap(find.text('Connect (Client)'));
    await tester.pumpAndSettle();

    expect(find.text('Disconnected'), findsOneWidget);
    expect(find.text('Connection Code'), findsOneWidget);
    expect(find.text('Paste connection code here'), findsOneWidget);
  });
}
