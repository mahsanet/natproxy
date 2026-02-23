import 'dart:math';

class NameGenerator {
  static final _random = Random();

  static const _adjectives = [
    'Swift',
    'Bold',
    'Brave',
    'Bright',
    'Calm',
    'Cool',
    'Cosmic',
    'Dark',
    'Divine',
    'Electric',
    'Fast',
    'Fierce',
    'Golden',
    'Happy',
    'Hidden',
    'Iron',
    'Lucky',
    'Magic',
    'Mighty',
    'Noble',
    'Quiet',
    'Quick',
    'Royal',
    'Silent',
    'Silver',
    'Smart',
    'Smooth',
    'Solar',
    'Sonic',
    'Steel',
    'Storm',
    'Strong',
    'Turbo',
    'Wild',
    'Wise',
  ];

  static const _nouns = [
    'Badger',
    'Bear',
    'Beaver',
    'Cheetah',
    'Cobra',
    'Condor',
    'Coyote',
    'Dragon',
    'Eagle',
    'Falcon',
    'Fox',
    'Hawk',
    'Jaguar',
    'Leopard',
    'Lion',
    'Lynx',
    'Mamba',
    'Orca',
    'Owl',
    'Panther',
    'Puma',
    'Raven',
    'Rhino',
    'Shark',
    'Tiger',
    'Viper',
    'Wolf',
    'Dolphin',
    'Phoenix',
    'Titan',
    'Warrior',
    'Knight',
    'Ranger',
    'Hunter',
    'Nomad',
  ];

  /// Generate a random display name in the format "Adjective Noun"
  /// e.g., "Swift Eagle", "Bold Dragon", "Silent Wolf"
  static String generate() {
    final adjective = _adjectives[_random.nextInt(_adjectives.length)];
    final noun = _nouns[_random.nextInt(_nouns.length)];
    return '$adjective $noun';
  }
}
