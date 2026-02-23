import 'package:flutter/material.dart';
import 'screens/home_screen.dart';
import 'screens/server_screen.dart';
import 'screens/client_screen.dart';
import 'screens/server_settings_screen.dart';
import 'screens/client_settings_screen.dart';

void main() {
  runApp(const MyApp());
}

class MyApp extends StatelessWidget {
  const MyApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'NATProxy',
      theme: ThemeData(
        colorScheme: ColorScheme.fromSeed(seedColor: Colors.deepPurple),
        useMaterial3: true,
      ),
      home: const HomeScreen(),
      routes: {
        '/server': (context) => const ServerScreen(),
        '/client': (context) => const ClientScreen(),
        '/server-settings': (context) => const ServerSettingsScreen(),
        '/client-settings': (context) => const ClientSettingsScreen(),
      },
    );
  }
}
