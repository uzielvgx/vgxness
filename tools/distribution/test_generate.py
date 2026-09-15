import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('generate', Path(__file__).with_name('generate.py'))
generate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(generate)

class DistributionTests(unittest.TestCase):
    def checksums(self):
        return '\n'.join(f'{str(i+1)*64}  vgxness_1.0.0_{target}.{suffix}' for i, (target, suffix) in enumerate(generate.TARGETS))

    def test_pinned_architectures(self):
        formula, manifest = generate.render('v1.0.0', self.checksums())
        self.assertIn('version "1.0.0"', formula)
        self.assertIn('bin.install "vgxness"', formula)
        for target, suffix in generate.TARGETS[:4]:
            self.assertIn(f'/v1.0.0/vgxness_1.0.0_{target}.{suffix}', formula)
        self.assertEqual(set(manifest['architecture']), {'64bit', 'arm64'})
        self.assertEqual(manifest['architecture']['arm64']['extract_dir'], 'vgxness_1.0.0_windows_arm64')
        self.assertNotIn('installer', manifest)

    def test_invalid_or_incomplete_metadata(self):
        for version in ['../v1.0.0', 'v1.0.0-alpha.1', '1.0.0', 'v01.0.0']:
            with self.assertRaises(ValueError): generate.render(version, self.checksums())
        for checksums in [self.checksums().split('\n', 1)[1], self.checksums() + '\n' + self.checksums(), self.checksums().replace('1'*64, 'invalid')]:
            with self.assertRaises(ValueError): generate.render('v1.0.0', checksums)

if __name__ == '__main__': unittest.main()
